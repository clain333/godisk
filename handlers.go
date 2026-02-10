package main

import (
	"database/sql"
	"errors"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/goccy/go-json"
	"github.com/h2non/filetype"
	"github.com/pquerna/otp/totp"
	"github.com/valyala/fasthttp"
)

func PageHandler(pageName string) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		authValue := ctx.Request.Header.Cookie(AuthKey)
		s := *(*string)(unsafe.Pointer(&authValue))
		if IsAuth {
			if s != AuthValue {
				ctx.Redirect("/login", fasthttp.StatusFound)
				return
			}
		}
		indexFilePath := filepath.Join(WORKDIR, "static", pageName)
		ctx.SendFile(indexFilePath)

	}
}
func LoginHandler(ctx *fasthttp.RequestCtx) {
	authValue := ctx.Request.Header.Cookie(AuthKey)
	s := *(*string)(unsafe.Pointer(&authValue))
	if !IsAuth {
		ctx.Redirect("/", 302)
	} else {
		if s == AuthValue {
			ctx.Redirect("/", 302)
			return
		}
		loginFilePath := filepath.Join(WORKDIR, "static", "login.html")
		ctx.SendFile(loginFilePath)
	}

}

func LoginAPIHandler(ctx *fasthttp.RequestCtx) {
	key := B2s(ctx.QueryArgs().Peek("key"))
	clientIP := ctx.RemoteIP().String()
	if totp.Validate(key, Secret) {
		DeleteIPRecord(clientIP)
		cookie := &fasthttp.Cookie{}
		cookie.SetKey(AuthKey)
		cookie.SetValue(AuthValue)
		cookie.SetPath("/")
		cookie.SetHTTPOnly(true)
		cookie.SetExpire(time.Now().AddDate(5, 0, 0))
		ctx.Response.Header.SetCookie(cookie)
		MsgResponse(ctx, OK, "登录成功")
	} else {
		IncrementIPCount(clientIP)

		failCount := GetIPCount(clientIP)

		if failCount >= 3 {
			AddToBloomFilter(clientIP)
			DeleteIPRecord(clientIP)
		}

		MsgResponse(ctx, FAIL, "密钥错误")
	}
}

func LoginOutAPIHandler(ctx *fasthttp.RequestCtx) {
	cookie := &fasthttp.Cookie{}
	cookie.SetKey(AuthKey)
	cookie.SetValue("")
	cookie.SetPath("/")
	cookie.SetExpire(time.Now().Add(-1 * time.Hour))
	cookie.SetMaxAge(-1)
	ctx.Response.Header.SetCookie(cookie)
	MsgResponse(ctx, OK, "退出成功")
}

func CleanFileHandler(ctx *fasthttp.RequestCtx) {
	key := B2s(ctx.QueryArgs().Peek("key"))
	if key != AdminKey {
		MsgResponse(ctx, FAIL, "key 错误")
		return
	}
	var names = make([]string, 0)
	rows, err := DB.Query("SELECT name FROM file_blob WHERE count <= 0")
	if err != nil {
		MsgResponse(ctx, FAIL, err.Error())
		log.Println(err)
		return
	}
	defer rows.Close()

	var name string
	for rows.Next() {
		if err := rows.Scan(&name); err != nil {
			MsgResponse(ctx, FAIL, err.Error())
			log.Println(err)
			return
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		MsgResponse(ctx, OK, "清除成功")
		return
	}
	for _, n := range names {
		if _, err := DB.Exec("DELETE FROM file_blob WHERE name = ?", n); err != nil {
			MsgResponse(ctx, FAIL, err.Error())
			log.Println(err)
			return
		}
		go DeleteFile("file", n)
		go DeleteFile("file", n+".fasthttp.br")
		go DeleteFile("file", n+".fasthttp.gz")
		go DeleteFile("file", n+".jpg")
	}
	MsgResponse(ctx, OK, "清除成功")
	return

}
func GetErrorHandler(ctx *fasthttp.RequestCtx) {
	key := B2s(ctx.QueryArgs().Peek("key"))
	if key != AdminKey {
		MsgResponse(ctx, FAIL, "key 错误")
		return
	}
	ctx.Response.Header.Set(
		"Content-Disposition",
		`attachment; filename="log.txt"`,
	)
	ctx.SetContentType("text/plain")
	dir := path.Join(WORKDIR, "log.txt")
	ctx.SendFile(dir)
}
func ClearErrorHandler(ctx *fasthttp.RequestCtx) {
	key := B2s(ctx.QueryArgs().Peek("key"))
	if key != AdminKey {
		MsgResponse(ctx, FAIL, "key 错误")
		return
	}
	err := F.Truncate(0)
	if err != nil {
		MsgResponse(ctx, FAIL, err.Error())
		log.Println(err)
		return
	}
	_, err = F.Seek(0, 0)
	if err != nil {
		MsgResponse(ctx, FAIL, err.Error())
		log.Println(err)
		return
	}
	MsgResponse(ctx, OK, "清除成功")
}

func GetSettingHandler(ctx *fasthttp.RequestCtx) {
	if IsAuth {
		MsgResponse(ctx, fasthttp.StatusAccepted, F2aUrl)
	} else {
		MsgResponse(ctx, OK, F2aUrl)
	}
}

func UpdataSecretHandler(ctx *fasthttp.RequestCtx) {
	key := B2s(ctx.QueryArgs().Peek("key"))
	if key != AdminKey {
		MsgResponse(ctx, FAIL, "key 错误")
		return
	}
	if err := UpDataQr(); err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		return
	}
	MsgResponse(ctx, OK, F2aUrl)
}
func UpdataAuthHandler(ctx *fasthttp.RequestCtx) {
	key := B2s(ctx.QueryArgs().Peek("key"))
	if key != AdminKey {
		MsgResponse(ctx, FAIL, "key 错误")
		return
	}
	IsAuth = !IsAuth
	MsgResponse(ctx, OK, "修改成功")
}
func GetStoreHandler(ctx *fasthttp.RequestCtx) {
	free, total, err := GetDeviceStorageInfo()
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	if total == 0 {
		MsgResponse(ctx, ERROR, "获取失败")
		log.Println(err)
		return
	}
	MsgResponse(ctx, OK, strconv.FormatUint(free*100/total, 10))
}

func CleanLimHandler(ctx *fasthttp.RequestCtx) {
	key := B2s(ctx.QueryArgs().Peek("key"))
	if key != AdminKey {
		MsgResponse(ctx, FAIL, "key 错误")
		return
	}
	MU.Lock()
	BloomFilter = bloom.NewWithEstimates(10000, 0.01)
	MU.Unlock()
	MsgResponse(ctx, OK, "清除成功")
}

func UpdataBkgHandler(ctx *fasthttp.RequestCtx) {
	key := B2s(ctx.QueryArgs().Peek("key"))
	if key != AdminKey {
		MsgResponse(ctx, FAIL, "key 错误")
		return
	}
	bkg := ctx.PostBody()

	if !filetype.IsImage(bkg) {
		MsgResponse(ctx, FAIL, "文件不是图片")
		return
	}
	filePath := path.Join(WORKDIR, "static", "bkg")
	file, err := os.Create(filePath)
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	defer file.Close()

	_, err = file.Write(bkg)
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	MsgResponse(ctx, OK, "修改成功")
}

// 获取文件 Handler
func GetFileHandler(ctx *fasthttp.RequestCtx) {
	id, err := fasthttp.ParseUint(ctx.QueryArgs().Peek("id"))
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}

	if !Exists("file", "id = ? AND is_dir = 1", id) {
		MsgResponse(ctx, fasthttp.StatusNotFound, "文件夹不存在")
		return
	}

	page, err := fasthttp.ParseUint(ctx.QueryArgs().Peek("page"))
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	if page == 0 {
		page = 1
	}
	offset := (page - 1) * 10
	rows, err := DB.Query(`SELECT f.id,f.lname,b.size, f.created_at,f.is_dir,b.mime FROM file f LEFT JOIN file_blob b ON f.name = b.name WHERE f.id > 1 AND f.parent_id = ? ORDER BY created_at DESC LIMIT 10 OFFSET ?
`, id, offset)
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	defer rows.Close()
	var idd, size uint64
	var createdAt int64
	var lname, mime string
	var isDir uint8
	rr := RespPool.Get().(*Resp)
	defer RespPool.Put(rr)
	*rr = Resp{}
	for rows.Next() {
		if err := rows.Scan(&idd, &lname, &size, &createdAt, &isDir, &mime); err != nil {
			MsgResponse(ctx, ERROR, err.Error())
			log.Println(err)
			return
		}
		rr.F = append(rr.F, FileInfo{
			ID:        idd,
			LName:     lname,
			Size:      size,
			IsDir:     isDir,
			Mime:      mime,
			CreatedAt: Timestamp10ToBJTime(createdAt),
		})
	}
	DB.QueryRow(`SELECT parent_id,lname FROM file WHERE id = ?`, id).Scan(&rr.ParentId, &rr.FolderName)
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetContentType("application/json; charset=utf-test.txt")
	buf := BufPool.Get().([]byte)
	buf = buf[:0]
	defer BufPool.Put(buf)
	buf, err = json.Marshal(rr)
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	ctx.SetBody(buf)

}

// 创建文件夹 Handler
func CreateFolderHandler(ctx *fasthttp.RequestCtx) {
	name := B2s(ctx.QueryArgs().Peek("name"))
	id, err := fasthttp.ParseUint(ctx.QueryArgs().Peek("parent_id"))
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	if !Exists("file", "id = ? AND is_dir = 1", id) {
		MsgResponse(ctx, FAIL, "文件夹不存在")
		return
	}
	if strings.ContainsAny(name, `/\:*?"<>|.。·、《》？：“{}】【；‘、·~&`) {
		MsgResponse(ctx, FAIL, "文件夹名称包含非法字符")
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		MsgResponse(ctx, FAIL, "文件夹名称不能为空")
		return
	}

	// 按字符数限制（支持中文）
	nameLen := utf8.RuneCountInString(name)
	if nameLen < 1 || nameLen > 24 {
		MsgResponse(ctx, FAIL, "文件夹名称长度需在 1-24 个字符之间")
		return
	}

	if Exists("file", "parent_id = ? AND lname = ? AND is_dir = 1 ", id, name) {
		MsgResponse(ctx, FAIL, "文件夹已存在")
		return
	}
	if _, err := DB.Exec(`INSERT INTO file (parent_id,lname,is_dir,created_at) values (?,?,?,?)`, id, name, 1, time.Now().Unix()); err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	MsgResponse(ctx, OK, "创建成功")
}
func CreateFileHandler(ctx *fasthttp.RequestCtx) {
	name := B2s(ctx.QueryArgs().Peek("name"))
	id, err := fasthttp.ParseUint(ctx.QueryArgs().Peek("parent_id"))
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	if !Exists("file", "id = ? AND is_dir = 1", id) {
		MsgResponse(ctx, FAIL, "文件夹不存在")
		return
	}
	if strings.ContainsAny(name, `/\:*?"<>|。、《》？：“{}】【；‘、·~&`) {
		MsgResponse(ctx, FAIL, "文件名称包含非法字符")
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		MsgResponse(ctx, FAIL, "文件名称不能为空")
		return
	}

	// 按字符数限制（支持中文）
	nameLen := utf8.RuneCountInString(name)
	if nameLen < 1 || nameLen > 24 {
		MsgResponse(ctx, FAIL, "文件名称长度需在 1-24 个字符之间")
		return
	}

	if Exists("file", "parent_id = ? AND lname = ? AND is_dir = 0 ", id, name) {
		MsgResponse(ctx, FAIL, "文件已存在")
		return
	}
	var names string

	if err := DB.QueryRow("SELECT name FROM file_blob WHERE size = 0 AND hash = 'd41d8cd98f00b204e9800998ecf8427e' LIMIT 1").Scan(&names); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			names = NextID()
			dir := path.Join(WORKDIR, "file", names)
			file, err := os.Create(dir)
			if err != nil {
				MsgResponse(ctx, ERROR, err.Error())
				log.Println(err)
				return
			}
			file.Close()
			if _, err := DB.Exec("INSERT INTO file_blob (name,hash,size,mime,count) VALUES (?,?,?,?,?);INSERT INTO file (parent_id,name,lname,is_dir,created_at) VALUES (?,?,?,?,?)", names, "d41d8cd98f00b204e9800998ecf8427e", 0, "text/plain", 1, id, names, name, 0, time.Now().Unix()); err != nil {
				MsgResponse(ctx, ERROR, err.Error())
				log.Println(err)
				go DeleteFile("file", names)
				return
			}
			MsgResponse(ctx, OK, "创建成功")
			return
		}
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	if _, err := DB.Exec("INSERT INTO file (parent_id,name,lname,is_dir,created_at) VALUES (?,?,?,?,?);UPDATE file_blob SET count = count+1 WHERE name = ?;", id, names, name, 0, time.Now().Unix(), names); err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	MsgResponse(ctx, OK, "创建成功")
}

// 预上传
func ReadyUploadHandler(ctx *fasthttp.RequestCtx) {
	name := B2s(ctx.QueryArgs().Peek("name"))
	id := B2s(ctx.QueryArgs().Peek("parent_id"))
	hash := B2s(ctx.QueryArgs().Peek("hash"))
	size, err := fasthttp.ParseUint(ctx.QueryArgs().Peek("size"))
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	if name == "" || id == "" || hash == "" || size < 0 {
		MsgResponse(ctx, FAIL, "数据不能为空")
		return
	}
	if !Exists("file", "id = ? AND is_dir = 1", id) {
		MsgResponse(ctx, FAIL, "文件夹不存在")
		return
	}
	if Exists("file", "parent_id = ? AND lname = ? AND is_dir = 0", id, name) {
		MsgResponse(ctx, FAIL, name+"已有同名文件")
		return
	}
	var names string
	if err = DB.QueryRow("SELECT name FROM file_blob WHERE hash = ? AND size = ? LIMIT 1", hash, size).Scan(&names); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			MsgResponse(ctx, OK, "0")
			return
		}
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	if _, err := DB.Exec(`INSERT INTO file (parent_id,name,lname,is_dir,created_at) values (?,?,?,?,?);UPDATE file_blob SET count = count +1 WHERE name = ?`, id, names, name, 0, time.Now().Unix(), names); err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	MsgResponse(ctx, OK, "1")
	return
}

func UploadFileHandler(ctx *fasthttp.RequestCtx) {
	name := B2s(ctx.QueryArgs().Peek("name"))
	id := B2s(ctx.QueryArgs().Peek("parent_id"))
	hash := B2s(ctx.QueryArgs().Peek("hash"))
	size, err := fasthttp.ParseUint(ctx.QueryArgs().Peek("size"))
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	names := NextID()
	dir := path.Join(WORKDIR, "file", names)
	ff, err := os.Create(dir)
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	defer ff.Close()

	_, err = io.Copy(ff, ctx.RequestBodyStream())
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}

	sizes, hashs, mime, err := GetFileSizeAndMD5(dir)
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		go DeleteFile("file", names)
		log.Println(err)
		return
	}
	if sizes != size {
		MsgResponse(ctx, FAIL, "文件大小错误")
		go DeleteFile("file", names)
		return
	}
	if hashs != hash {
		MsgResponse(ctx, FAIL, "文件内容错误")
		go DeleteFile("file", names)
		return
	}
	if Exists("file", "parent_id = ? AND lname = ? AND is_dir = 0", id, name) {
		MsgResponse(ctx, FAIL, "已有同名文件")
		go DeleteFile("file", names)
		return
	}
	var n string
	if err = DB.QueryRow("SELECT name FROM file_blob WHERE hash = ? AND size = ? LIMIT 1", hash, size).Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := DB.Exec("INSERT INTO file_blob (name,hash,size,mime,count) VALUES (?,?,?,?,?);INSERT INTO file (parent_id,name,lname,is_dir,created_at) VALUES (?,?,?,?,?)", names, hash, size, mime, 1, id, names, name, 0, time.Now().Unix()); err != nil {
				MsgResponse(ctx, ERROR, err.Error())
				log.Println(err)
				return
			}
			go CreateImaging(dir)
			MsgResponse(ctx, OK, "上传成功")
			return
		}
		go DeleteFile("file", names)
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	if _, err := DB.Exec("UPDATE file_blob SET count = count + 1 WHERE name = ? ;INSERT INTO file (parent_id,name,lname,is_dir,created_at) VALUES (?,?,?,?,?)", n, id, n, name, "", 0, 0, time.Now().Unix()); err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		go DeleteFile("file", names)
		return
	}
	go DeleteFile("file", names)
	MsgResponse(ctx, OK, "上传成功")
	return
}

// 下载文件 Handler
func DownloadFileHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))
	var name, lname string
	if err := DB.QueryRow("SELECT name,lname FROM file WHERE id = ?", id).Scan(&name, &lname); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			MsgResponse(ctx, FAIL, "文件不存在")
			return
		}
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	dir := path.Join(WORKDIR, "file", name)
	ctx.Response.Header.Set(
		"Content-Disposition",
		`attachment; filename="`+lname+`"`,
	)
	ctx.SetContentType("application/octet-stream")
	ctx.SendFile(dir)
}

// 删除文件 Handler
func DeleteFileHandler(ctx *fasthttp.RequestCtx) {
	id, err := fasthttp.ParseUint(ctx.QueryArgs().Peek("id"))
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		return
	}
	if id == 1 {
		MsgResponse(ctx, FAIL, "不允许删除根目录")
		return
	}
	if !Exists("file", "id = ? ", id) {
		MsgResponse(ctx, FAIL, "文件不存在")
		return
	}
	select {
	case AsyncDel <- id:
		MsgResponse(ctx, OK, "删除请求成功")
	default:
		MsgResponse(ctx, FAIL, "请求已满稍后重试")
	}
	return
}

func PreviewFileHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))
	var name string
	if err := DB.QueryRow("SELECT name FROM file  WHERE id = ? LIMIT 1", id).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			MsgResponse(ctx, FAIL, "文件不存在")
			return
		}
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	filePath := filepath.Join(WORKDIR, "file", name)
	ctx.SendFile(filePath)
}
func PreviewImagingHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))
	var name string
	if err := DB.QueryRow("SELECT name FROM file  WHERE id = ? LIMIT 1", id).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			MsgResponse(ctx, FAIL, "文件不存在")
			return
		}
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	filePath := filepath.Join(WORKDIR, "file", name+".jpg")
	ctx.SendFile(filePath)
}

func SaveFileHandler(ctx *fasthttp.RequestCtx) {
	name := NextID()
	dir := path.Join(WORKDIR, "file", name)
	ff, err := os.Create(dir)
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}

	_, err = io.Copy(ff, ctx.RequestBodyStream())
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		ff.Close()
		log.Println(err)
		return
	}
	ff.Close()
	size, hash, mime, err := GetFileSizeAndMD5(dir)
	if err != nil {
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	id := B2s(ctx.QueryArgs().Peek("id"))
	var n string
	if err := DB.QueryRow("SELECT name FROM file WHERE id = ? LIMIT 1", id).Scan(&n); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			MsgResponse(ctx, FAIL, "文件不存在")
			go DeleteFile("file", name)
			return
		}
		go DeleteFile("file", name)
		MsgResponse(ctx, ERROR, err.Error())
		log.Println(err)
		return
	}
	var nn string
	if err := DB.QueryRow("SELECT name FROM file_blob WHERE hash = ? AND size = ? LIMIT 1", hash, size).Scan(&nn); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := DB.Exec("INSERT INTO file_blob (name,hash,size,mime,count) VALUES (?,?,?,?,?);UPDATE file SET name = ? WHERE id = ?;UPDATE file_blob SET count = count-1 WHERE name = ?", name, hash, size, mime, 1, name, id, n); err != nil {
				MsgResponse(ctx, ERROR, err.Error())
				log.Println(err)
				go DeleteFile("file", n)
				return
			}
			MsgResponse(ctx, OK, "保存成功")
			return
		}
		go DeleteFile("file", name)
		MsgResponse(ctx, FAIL, err.Error())
		log.Println(err)
		return
	}
	go DeleteFile("file", name)
	if _, err := DB.Exec("UPDATE file_blob SET count = count + 1 WHERE name = ?;UPDATE file_blob SET count = count - 1 WHERE name = ?;UPDATE file SET name = ? WHERE id = ? ", nn, n, nn, id); err != nil {
		MsgResponse(ctx, FAIL, err.Error())
		log.Println(err)
		return
	}
	MsgResponse(ctx, OK, "保存成功")
}
