package main

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/goccy/go-json"
	"github.com/valyala/fasthttp"
)

func PageHandler(pageName string) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		authValue := ctx.Request.Header.Cookie("Authorization")
		s := *(*string)(unsafe.Pointer(&authValue))
		if s == AuthValue {
			indexFilePath := filepath.Join(AppConfig.WorkDir, "templates", pageName)
			ctx.SendFile(indexFilePath)
		} else {
			ctx.Redirect("/login", fasthttp.StatusFound)
		}
	}
}
func LoginHandler(ctx *fasthttp.RequestCtx) {
	authValue := ctx.Request.Header.Cookie("Authorization")
	s := *(*string)(unsafe.Pointer(&authValue))
	if s == AuthValue {
		ctx.Redirect("/", 302)
		return
	}
	loginFilePath := filepath.Join(AppConfig.WorkDir, "templates", "login.html")
	ctx.SendFile(loginFilePath)
}
func LoginAPIHandler(ctx *fasthttp.RequestCtx) {
	var requestData struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(ctx.PostBody(), &requestData); err != nil {
		JSONResponse(ctx, FAIL, "请求格式错误", nil)
		log.Println(err)
		return
	}

	clientIP := ctx.RemoteIP().String()
	if requestData.Password == AppConfig.Password {
		DeleteIPRecord(clientIP)
		cookie := fasthttp.AcquireCookie()   // 获取一个 cookie 对象
		defer fasthttp.ReleaseCookie(cookie) // 使用完释放

		cookie.SetKey("Authorization")
		cookie.SetValue(AuthValue)
		cookie.SetPath("/")
		cookie.SetHTTPOnly(true)
		cookie.SetExpire(time.Now().AddDate(1, 0, 0))

		ctx.Response.Header.SetCookie(cookie)
		JSONResponse(ctx, SUCCESSCODE, "登录成功", nil)
	} else {
		IncrementIPCount(clientIP)

		// 获取当前失败次数
		failCount := GetIPCount(clientIP)

		// 如果失败次数达到3次，则加入布隆过滤器并删除map记录
		if failCount >= 3 {
			AddToBloomFilter(clientIP)
			DeleteIPRecord(clientIP)
		}

		JSONResponse(ctx, FAIL, "密码错误", nil)
	}
}

func LoginOutAPIHandler(ctx *fasthttp.RequestCtx) {
	cookie := &fasthttp.Cookie{}
	cookie.SetKey("Authorization")
	cookie.SetValue("")
	cookie.SetPath("/")
	cookie.SetExpire(time.Now().Add(-1 * time.Hour))
	cookie.SetMaxAge(-1)
	ctx.Response.Header.SetCookie(cookie)
	JSONResponse(ctx, SUCCESSCODE, "退出成功", nil)
}

// StoreinfoHandler 存储信息处理器
func StoreinfoHandler(ctx *fasthttp.RequestCtx) {
	// 获取存储空间信息
	var Data struct {
		Total uint64 `json:"total"`
		Free  uint64 `json:"free"`
	}
	var err error
	Data.Total, Data.Free, err = GetDeviceStorageInfo()
	if err != nil {
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	JSONResponse(ctx, SUCCESSCODE, "获取成功", Data)
}

// 修改密码 Handler
func ChangeSettingHandler(ctx *fasthttp.RequestCtx) {
	var req struct {
		Password      string `json:"password"`
		MaxShareMb    uint64 `json:"max_share_mb"`
		MaxUploadGb   uint64 `json:"max_upload_gb"`
		DeadlineStore uint64 `json:"deadline_store"`
	}
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		JSONResponse(ctx, FAIL, "请求失败", nil)
		return
	}
	MU.Lock()
	if req.Password != "" {
		AppConfig.Password = req.Password
		AuthValue = string(GenerateRandomString(54))
	}
	if req.MaxShareMb <= 0 {
		AppConfig.MaxShareSize = req.MaxShareMb
	}
	if req.MaxUploadGb <= 0 {
		AppConfig.MaxUploadSize = req.MaxUploadGb
	}
	if req.DeadlineStore <= 0 {
		AppConfig.DeadlineStore = req.DeadlineStore
	}
	MU.Unlock()
	JSONResponse(ctx, SUCCESSCODE, "修改成功", nil)
}

// 获取文件 Handler
func GetFileHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))

	if !Exists("logical", "id = ? AND is_dir = 1", id) {
		JSONResponse(ctx, FAIL, "文件夹不存在", nil)
		return
	}

	page, err := fasthttp.ParseUint(ctx.QueryArgs().Peek("page"))
	if err != nil {
		JSONResponse(ctx, FAIL, "page错误", nil)
		return
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * 10
	rows, err := DB.Query(`SELECT id,name,size,created_at,is_dir,is_share FROM logical WHERE id > 1 AND parent_id = ? ORDER BY created_at DESC LIMIT 10 OFFSET ?`, id, offset)
	if err != nil {
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	defer rows.Close()
	var idd, size uint64
	var created_at int64
	var name string
	var is_dir, is_share bool
	rr := Resp1Pool.Get().(*Resp1)
	defer Resp1Pool.Put(rr)
	*rr = Resp1{}
	for rows.Next() {
		if err := rows.Scan(&idd, &name, &size, &created_at, &is_dir, &is_share); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		rr.F = append(rr.F, FileInfo{
			ID:        idd,
			Name:      name,
			Size:      size,
			IsDir:     is_dir,
			IsShare:   is_share,
			CreatedAt: Timestamp10ToBJTime(created_at),
		})
	}
	DB.QueryRow(`SELECT parent_id FROM logical WHERE id = ?`, id).Scan(&rr.ParentId)

	JSONResponse(ctx, SUCCESSCODE, "获取成功", rr)
}

// 创建文件夹 Handler
func CreateFolderHandler(ctx *fasthttp.RequestCtx) {
	var req struct {
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
	}
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		JSONResponse(ctx, FAIL, "请求格式错误", nil)
		return
	}
	if strings.ContainsAny(req.Name, `/\:*?"<>|`) {
		JSONResponse(ctx, FAIL, "文件夹名称包含非法字符", nil)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		JSONResponse(ctx, FAIL, "文件夹名称不能为空", nil)
		return
	}

	// 按字符数限制（支持中文）
	nameLen := utf8.RuneCountInString(name)
	if nameLen < 1 || nameLen > 50 {
		JSONResponse(ctx, FAIL, "文件夹名称长度需在 1-50 个字符之间", nil)
		return
	}

	if Exists("logical", "parent_id = ? AND name = ? AND is_dir = 1 ", req.ParentID, name) {
		JSONResponse(ctx, FAIL, "文件夹已存在", nil)
		return
	}
	var paths string
	err := DB.QueryRow(`SELECT path FROM logical WHERE id = ? LIMIT 1`, req.ParentID).Scan(&paths)
	if err != nil {
		if err == sql.ErrNoRows {
			JSONResponse(ctx, FAIL, "文件夹不存在", nil)
			return
		}
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	paths = JoinStrings(paths, "-", req.ParentID)

	if _, err := DB.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?)`, req.ParentID, paths, name, 0, 0, 1, 0, "", time.Now().Unix()); err != nil {
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	JSONResponse(ctx, SUCCESSCODE, "创建成功", nil)
}

// 预上传
func ReadyUploadHandler(ctx *fasthttp.RequestCtx) {
	var req struct {
		Name     string `json:"name"`
		ParentID string `json:"parent_id"`
		FileSize uint64 `json:"file_size"`
		FileHash string `json:"file_hash"`
	}

	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		JSONResponse(ctx, FAIL, "请求格式错误", nil)
		log.Println(err)
		return
	}
	if req.Name == "" || req.ParentID == "" {
		JSONResponse(ctx, FAIL, "数据不能为空", nil)
		return
	}
	if req.FileSize > AppConfig.MaxUploadSize*1024*1024*1024 {
		JSONResponse(ctx, FAIL, "文件过大", nil)
		return
	}
	if !Exists("logical", "id = ? AND is_dir = 1", req.ParentID) {
		JSONResponse(ctx, FAIL, "文件夹不存在", nil)
		return
	}

	if Exists("logical", "parent_id = ? AND name = ? AND is_dir = 0", req.ParentID, req.Name) {
		JSONResponse(ctx, FAIL, "已有同名文件", nil)
		return
	}
	var name string
	err := DB.QueryRow("SELECT name FROM file WHERE hash = ? AND size = ? LIMIT 1", req.FileHash, req.FileSize).Scan(&name)
	if err == nil {
		var paths string
		if err := DB.QueryRow("SELECT path FROM logical WHERE id = ? LIMIT 1", req.ParentID).Scan(&paths); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		paths = JoinStrings(paths, "-", req.ParentID)

		if _, err := DB.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?);UPDATE file SET ref_count = ref_count + 1 WHERE hash = ?;`, req.ParentID, paths, req.Name, req.FileSize, name, 0, 0, "", time.Now().Unix(), req.FileHash); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		JSONResponse(ctx, SUCCESSCODE, "上传成功", true)
		return
	} else {
		if errors.Is(err, sql.ErrNoRows) {
			_, freeint, err := GetDeviceStorageInfo()
			if err != nil {
				log.Println(err)
				JSONResponse(ctx, ERROR, "", nil)
				return
			}
			if freeint-req.FileSize < AppConfig.DeadlineStore*1024*1024*1024 {
				JSONResponse(ctx, FAIL, "设备存储空间不足", nil)
				return
			}
			size, count := CalculateOptimalPart(req.FileSize)
			ucount := count / 8
			if count%8 != 0 {
				ucount = ucount + 1
			}
			var f = &FileContext{
				Code:         NextID(),
				Name:         req.Name,
				ParentId:     req.ParentID,
				FileSize:     req.FileSize,
				FileHash:     req.FileHash,
				IsLive:       true,
				MaxChunkSize: size,
				ChunkCount:   count,
				UploadCount:  make([]uint8, ucount),
			}

			var resp struct {
				Code       string `json:"code"`
				ChunkCount uint64 `json:"chunk_count"`
				ChunkSize  uint64 `json:"chunk_size"`
			}
			resp.Code = f.Code
			resp.ChunkCount = f.ChunkCount
			resp.ChunkSize = f.MaxChunkSize
			UploadMapMu.Lock()
			UploadMap[f.Code] = f
			UploadMapMu.Unlock()
			JSONResponse(ctx, SUCCESSCODE, "初始化成功", resp)
			return
		}
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
}

// 上传文件 Handler
func UploadFileHandler(ctx *fasthttp.RequestCtx) {
	Code := string(ctx.Request.Header.Peek("X-File-Code"))
	index, err := fasthttp.ParseUint(ctx.Request.Header.Peek("X-File-Index"))
	if err != nil {
		JSONResponse(ctx, ERROR, "", nil)
		return
	}
	UploadMapMu.Lock()
	f, ok := UploadMap[Code]
	UploadMapMu.Unlock()
	if !ok {
		JSONResponse(ctx, FAIL, "未找到文件信息", nil)
		return
	}
	if index == 0 {
		size, hash, mime, _, err := GetFileSizeAndMD5(f.Code)
		if err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		if size != int64(f.FileSize) || hash != f.FileHash {
			JSONResponse(ctx, FAIL, "文件信息错误", nil)
			return
		}
		if !Exists("logical", "id = ? AND is_dir = 1", f.ParentId) {
			JSONResponse(ctx, FAIL, "文件夹不存在", nil)
			return
		}
		if Exists("logical", "parent_id = ? AND name = ? AND is_dir = 0", f.ParentId, f.Name) {
			JSONResponse(ctx, FAIL, "已有同名文件", nil)
			return
		}
		tx, err := DB.Begin()
		if err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		defer tx.Rollback()
		_, err = tx.Exec(`INSERT INTO file (name,hash,size,mime,ref_count,created_at) values (?,?,?,?,?,?);`, f.Code, f.FileHash, f.FileSize, mime, 1, time.Now().Unix())
		if err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		var paths string
		if err := tx.QueryRow(`SELECT path FROM logical WHERE id = ? LIMIT 1`, f.ParentId).Scan(&paths); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		paths = JoinStrings(paths, "-", f.ParentId)
		if _, err := tx.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?);`, f.ParentId, paths, f.Name, f.FileSize, f.Code, 0, 0, "", time.Now().Unix()); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		if err := tx.Commit(); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		UploadMapMu.Lock()
		delete(UploadMap, f.Code)
		UploadMapMu.Unlock()
		JSONResponse(ctx, SUCCESSCODE, "上传成功", nil)
		return
	}
	if IsBitSet(f.UploadCount[(index-1)/8], (index-1)%8) {
		JSONResponse(ctx, FAIL, "该片已上传过", nil)
		return
	}
	if len(ctx.PostBody()) > int(f.MaxChunkSize) {
		JSONResponse(ctx, FAIL, "单片size不对", nil)
		return
	}
	if int64(index) > int64(f.ChunkCount) {
		JSONResponse(ctx, FAIL, "index错误", nil)
		return
	}

	dir := path.Join(AppConfig.WorkDir, "upload", f.Code)
	fff, err := os.OpenFile(dir, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	defer fff.Close()
	offset := int64(index-1) * int64(f.MaxChunkSize)
	_, err = fff.WriteAt(ctx.PostBody(), offset)
	if err != nil {
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	UploadMapMu.Lock()
	UploadMap[f.Code].IsLive = true
	SetBit(&UploadMap[f.Code].UploadCount[(index-1)/8], (index-1)%8)
	UploadMapMu.Unlock()
	JSONResponse(ctx, SUCCESSCODE, "分片上传成功", nil)
}

// 创建便签 Handler
func CreateNoteHandler(ctx *fasthttp.RequestCtx) {
	var req struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		JSONResponse(ctx, FAIL, "请求格式错误", nil)
		return
	}
	if req.Title == "" || req.Text == "" {
		JSONResponse(ctx, FAIL, "错误的请求", nil)
		return
	}
	if Exists("note", "title = ?", req.Title) {
		if _, err := DB.Exec(`UPDATE note set text = ? , created_at = ? WHERE title = ?`, req.Text, time.Now().Unix(), req.Title); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		JSONResponse(ctx, SUCCESSCODE, "更新成功", nil)
		return
	}
	if _, err := DB.Exec("INSERT INTO note (text,title,created_at) values (?,?,?)", req.Text, req.Title, time.Now().Unix()); err != nil {
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	JSONResponse(ctx, SUCCESSCODE, "创建成功", nil)
	return

}

// 删除便签 Handler
func DeleteNoteHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))
	if _, err := DB.Exec("DELETE FROM note WHERE id = ?", id); err != nil {
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	JSONResponse(ctx, SUCCESSCODE, "删除成功", nil)
}

// 获取便签
func ViewNoteHandler(ctx *fasthttp.RequestCtx) {
	page, err := fasthttp.ParseUint(ctx.QueryArgs().Peek("page"))
	if err != nil {
		JSONResponse(ctx, FAIL, "page错误", nil)
		return
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * 5
	rr := Resp2Pool.Get().([]Resp2)
	defer Resp2Pool.Put(rr)
	rr = rr[:0]
	rows, err := DB.Query(`SELECT id,text,title,created_at FROM note  ORDER BY created_at DESC LIMIT 5 OFFSET ?`, offset)
	if err != nil {
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	defer rows.Close()
	var text, title string
	var createAt, id int64
	for rows.Next() {
		if err := rows.Scan(&id, &text, &title, &createAt); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		rr = append(rr, Resp2{
			Title:     title,
			Text:      text,
			CreatedAt: Timestamp10ToBJTime(createAt),
			ID:        id,
		})
	}
	JSONResponse(ctx, SUCCESSCODE, "获取成功", rr)
}

// 分享文件 Handler
func ShareFileHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))

	var isShare bool
	if err := DB.QueryRow("SELECT is_share FROM logical WHERE id = ? AND is_dir = 0 LIMIT 1", id).Scan(&isShare); err == nil {
		if isShare {
			if _, err := DB.Exec("UPDATE logical SET is_share = ? WHERE id = ? ", 0, id); err != nil {
				JSONResponse(ctx, FAIL, "", nil)
				log.Println(err)
				return
			}
			JSONResponse(ctx, SUCCESSCODE, "取消分享成功", nil)
			return
		} else {
			if _, err := DB.Exec("UPDATE logical SET is_share = ? , pwd = ? WHERE id = ? ", 1, GenerateRandomString(6), id); err != nil {
				JSONResponse(ctx, FAIL, "", nil)
				log.Println(err)
				return
			}
			JSONResponse(ctx, SUCCESSCODE, "分享成功", nil)
			return
		}
	} else {
		if errors.Is(err, sql.ErrNoRows) {
			JSONResponse(ctx, FAIL, "文件不存在", nil)
			return
		}
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
}

// 查看分享的文件 Handler
func ViewShareFileHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))
	var pwd string
	var ok bool
	if err := DB.QueryRow("SELECT pwd,is_share FROM logical WHERE id = ? AND is_dir = 0 LIMIT 1", id).Scan(&pwd, &ok); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			JSONResponse(ctx, FAIL, "文件不存在", nil)
			return
		}
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	if !ok {
		JSONResponse(ctx, FAIL, "文件未被分享", nil)
		return
	}
	JSONResponse(ctx, SUCCESSCODE, "获取成功", pwd)
	return

}

// 下载分享文件 Handler
func DownloadShareHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))
	Pwd := B2s(ctx.QueryArgs().Peek("pwd"))
	var filename, pwd, logicalName string
	var size uint64
	if err := DB.QueryRow(`SELECT name,file_name,pwd,size FROM logical WHERE id = ? AND is_share = 1 AND is_dir = 0 LIMIT 1`, id).Scan(&logicalName, &filename, &pwd, &size); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			JSONResponse(ctx, FAIL, "文件未分享", nil)
			return
		}
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	if Pwd != pwd {
		JSONResponse(ctx, FAIL, "密码错误", nil)
		return
	}

	if size > AppConfig.MaxShareSize*1024*1024 {
		JSONResponse(ctx, FAIL, "文件过大", nil)
		return
	}
	dir := path.Join(AppConfig.WorkDir, "upload", filename)
	ctx.Response.Header.Set(
		"Content-Disposition",
		`attachment; filename="`+logicalName+`"`,
	)
	ctx.SetUserValue("size", size)
	ctx.SetContentType("application/octet-stream")
	ctx.SendFile(dir)
}

// 下载文件 Handler
func DownloadFileHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))
	var logicalName, fileName string
	if err := DB.QueryRow("SELECT file_name,name FROM logical WHERE id = ? LIMIT 1", id).Scan(&fileName, &logicalName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			JSONResponse(ctx, FAIL, "文件不存在", nil)
			return
		}
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	dir := path.Join(AppConfig.WorkDir, "upload", fileName)

	ctx.Response.Header.Set(
		"Content-Disposition",
		`attachment; filename="`+logicalName+`"`,
	)
	ctx.SetContentType("application/octet-stream")
	ctx.SendFile(dir)
}

// 发送直链 Handler
func OffloneDowloadHandler(ctx *fasthttp.RequestCtx) {
	var req struct {
		Url     string `json:"url"`
		Headers []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"headers"`
	}
	if err := json.Unmarshal(ctx.PostBody(), &req); err != nil {
		JSONResponse(ctx, FAIL, "请求格式错误", nil)
		return
	}

	var header = map[string]string{}
	for _, value := range req.Headers {
		header[value.Key] = value.Value
	}
	name := NextID()
	go DownloadToFile(name, req.Url, header)
	JSONResponse(ctx, SUCCESSCODE, "创建成功", name)
}

// 删除文件 Handler
func DeleteFileHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))
	var is_dir bool
	var name, filename string
	if err := DB.QueryRow(`SELECT is_dir,name,file_name FROM logical WHERE id = ? LIMIT 1`, id).Scan(&is_dir, &name, &filename); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			JSONResponse(ctx, FAIL, "文件或文件夹不存在", nil)
			return
		}
		JSONResponse(ctx, ERROR, "", nil)
		log.Println(err)
		return
	}
	if is_dir {
		tx, err := DB.Begin()
		if err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		defer tx.Rollback()
		var paths string
		if err := tx.QueryRow(`SELECT path FROM logical WHERE id = ? LIMIT 1`, id).Scan(&paths); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		paths = fmt.Sprintf("%s-%s%%", paths, id)

		rows, err := tx.Query(`SELECT file_name FROM logical WHERE path LIKE ? AND is_dir = 0`, paths)
		if err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		defer rows.Close()
		var filenames = []string{}

		for rows.Next() {
			if err := rows.Scan(&filename); err != nil {
				JSONResponse(ctx, ERROR, "", nil)
				log.Println(err)
				return
			}
			filenames = append(filenames, filename)
		}
		if _, err := tx.Exec("DELETE FROM logical WHERE id = ?", id); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		if _, err := tx.Exec("DELETE FROM logical WHERE path LIKE ?", paths); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		placeholders := make([]string, len(filenames))
		args := make([]interface{}, len(filenames))
		for i, v := range filenames {
			placeholders[i] = "?"
			args[i] = v
		}
		sql := fmt.Sprintf("UPDATE file SET ref_count = ref_count - 1 WHERE name IN (%s)", strings.Join(placeholders, ","))
		if _, err := tx.Exec(sql, args...); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		if err := tx.Commit(); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		JSONResponse(ctx, SUCCESSCODE, "删除成功", nil)
		return
	} else {
		if _, err := DB.Exec(`
		DELETE FROM logical WHERE id = ?;
		UPDATE file SET ref_count = ref_count - 1 WHERE name = ?;`, id, filename); err != nil {
			JSONResponse(ctx, ERROR, "", nil)
			log.Println(err)
			return
		}
		JSONResponse(ctx, SUCCESSCODE, "删除成功", nil)
		return

	}
}

func PreviewFileHandHandler(ctx *fasthttp.RequestCtx) {
	id := B2s(ctx.QueryArgs().Peek("id"))

	var Info struct {
		FName string `json:"fname"`
		LName string `json:"lname"`
		Mime  string `json:"mime"`
	}
	// 2. 查询数据库（[]byte 可直接作为参数）
	err := DB.QueryRow(`
		SELECT f.name, l.name, f.mime
		FROM logical l
		LEFT JOIN file f ON l.file_name = f.name
		WHERE l.id = ?
	`, id).Scan(&Info.FName, &Info.LName, &Info.Mime)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			JSONResponse(ctx, FAIL, "文件记录不存在", nil)
			return
		}
		log.Println("DB Query Error:", err)
		JSONResponse(ctx, ERROR, "", nil)
		return
	}
	if Info.Mime == "application/octet-stream" {
		Info.Mime = GuessMimeByExt(Info.LName)
	}
	JSONResponse(ctx, SUCCESSCODE, "", Info)
}
func PreviewFileHandler(ctx *fasthttp.RequestCtx) {
	name := string(ctx.QueryArgs().Peek("name"))

	filePath := filepath.Join(AppConfig.WorkDir, "upload", name)

	ctx.SendFile(filePath)
}

// MIME 兜底函数
