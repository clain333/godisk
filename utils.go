package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/goccy/go-json"
	"github.com/h2non/filetype"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/valyala/fasthttp"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var (
	IpMap     = make(map[string]uint8)
	IpMutex   sync.RWMutex
	IdCounter int64
)

// 布隆过滤器变量
var bloomFilter *bloom.BloomFilter

// 初始化布隆过滤器
func InitBloomFilter() {
	// 创建一个能够容纳10000个元素且误判率为0.01的布隆过滤器
	bloomFilter = bloom.NewWithEstimates(10000, 0.01)
}

// 向布隆过滤器添加元素
func AddToBloomFilter(item string) {
	if bloomFilter != nil {
		bloomFilter.Add([]byte(item))
	}
}

// 棜查布隆过滤器中是否存在元素
func CheckInBloomFilter(item string) bool {
	if bloomFilter == nil {
		return false
	}
	return bloomFilter.Test([]byte(item))
}

// 增加IP访问次数
func IncrementIPCount(ip string) {
	IpMutex.Lock()
	defer IpMutex.Unlock()
	IpMap[ip]++
}

// 获取IP访问次数
func GetIPCount(ip string) uint8 {
	IpMutex.RLock()
	defer IpMutex.RUnlock()
	return IpMap[ip]
}

// 删除IP记录
func DeleteIPRecord(ip string) {
	IpMutex.Lock()
	defer IpMutex.Unlock()
	delete(IpMap, ip)
}

func GenerateRandomString(length int) []byte {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return b
}

// GetDeviceStorageInfo 获取设备全部存储空间信息
func GetDeviceStorageInfo() (total uint64, free uint64, err error) {
	usage, err := disk.Usage("/")
	if err != nil {
		usage, err = disk.Usage("C:")
		if err != nil {
			return 0, 0, err
		}
	}
	return usage.Total, usage.Free, nil
}

// DeleteFile 物理删除文件
func DeleteFile(filename string) {
	paths := filepath.Join(AppConfig.WorkDir, "upload", filename)
	err := os.Remove(paths)
	if err != nil {
		time.Sleep(5 * time.Second)
		err = os.Remove(paths)
		if err != nil {
			log.Println(err)
			return
		}
	}
}

func JSONResponse(ctx *fasthttp.RequestCtx, code int, message string, data interface{}) {
	ctx.SetStatusCode(fasthttp.StatusOK)
	ctx.SetContentType("application/json; charset=utf-8")
	reresultsp := JsonRespPool.Get().(*JSONResult)
	defer JsonRespPool.Put(reresultsp)
	reresultsp.Message = message
	reresultsp.Data = data
	buf := BufPool.Get().([]byte)
	buf = buf[:0]
	defer BufPool.Put(buf)

	b, _ := json.Marshal(reresultsp)
	buf = append(buf, b...)
	ctx.SetStatusCode(code)
	ctx.SetBody(buf)
}
func Timestamp10ToBJTime(ts int64) string {
	return time.Unix(ts, 0).
		In(time.FixedZone("CST", 8*3600)).
		Format("2006年01月02日 15时04分05秒")
}

func B2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
func GetFileSizeAndMD5(fileName string) (int64, string, string, string, error) {
	filepath := filepath.Join(AppConfig.WorkDir, "upload", fileName)
	file, err := os.Open(filepath)
	if err != nil {
		return 0, "", "", "", err
	}
	defer file.Close()

	hash := md5.New()
	header := make([]byte, 512)

	// 读取文件，同时计算 MD5，并截取前 512 字节用于 MIME 判断
	buffer := make([]byte, 4096)
	totalSize := int64(0)
	for {
		readN, readErr := file.Read(buffer)
		if readN > 0 {
			if totalSize < 512 {
				end := int64(readN)
				if totalSize+end > 512 {
					end = 512 - totalSize
				}
				copy(header[totalSize:], buffer[:end])
			}
			totalSize += int64(readN)
			if _, err := hash.Write(buffer[:readN]); err != nil {
				return 0, "", "", "", err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return 0, "", "", "", readErr
		}
	}

	hashStr := hex.EncodeToString(hash.Sum(nil))

	// MIME 判断
	var mime, ext string
	if kind, err := filetype.Match(header[:minInt(int(totalSize), 512)]); err == nil && kind != filetype.Unknown {
		mime = kind.MIME.Value
		ext = kind.Extension
	} else {
		mime = "application/octet-stream"
		ext = "txt"
	}

	return totalSize, hashStr, mime, ext, nil
}

// 辅助函数
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Exists(table, query string, args ...any) bool {
	var dummy int
	err := DB.QueryRow(
		fmt.Sprintf("SELECT 1 FROM %s WHERE %s LIMIT 1", table, query),
		args...,
	).Scan(&dummy)
	return err == nil
}

func NextID() string {
	id := atomic.AddInt64(&IdCounter, 1)
	return strconv.FormatInt(id, 10)
}

func SetBit(bitmap *uint8, pos int) {
	if pos >= 8 {
		return // 超出 uint8 范围，直接返回
	}
	*bitmap |= 1 << pos
}

// IsBitSet 检测 bitmap 的指定位置 pos 是否为 1
func IsBitSet(bitmap uint8, pos int) bool {
	if pos >= 8 {
		return false // 超出范围，返回 false
	}
	return bitmap&(1<<pos) != 0
}
func JoinWithCommasAndParens(arr []string) string {
	if len(arr) == 0 {
		return "()" // 空数组返回 ()
	}
	joined := strings.Join(arr, ",") // 用逗号拼接
	return "(" + joined + ")"        // 前后加括号
}
func WriteStringToFile(filename, content string) error {
	dir := path.Join(AppConfig.WorkDir, "upload", filename)
	err := os.WriteFile(dir, []byte(content), 0644)
	if err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}
func JoinStrings(parts ...string) string {
	var sb strings.Builder
	for _, part := range parts {
		sb.WriteString(part)
	}
	return sb.String()
}

func GuessMimeByExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".txt", ".md", ".go", ".py", ".java", ".js", ".json":
		return "text/plain"
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/docx"
	case ".xlsx":
		return "application/xlsx"
	case ".pptx":
		return "application/pptx"
	case ".zip":
		return "application/zip"
	case ".rar":
		return "application/vnd.rar"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".mp4":
		return "video/mp4"
	case ".flac":
		return "audio/x-flac"
	default:
		return "text/plain"
	}
}
func CalculateOptimalPart(fileSizeBytes uint64) (partSizeBytes uint64, parts uint64) {
	fileSizeMB := fileSizeBytes / MB

	switch {
	case fileSizeMB < 100:
		partSizeBytes = 10 * MB
	case fileSizeMB >= 100 && fileSizeMB < 1024:
		partSizeBytes = 50 * MB
	case fileSizeMB >= 1024 && fileSizeMB < 51200:
		partSizeBytes = 100 * MB
	default:
		partSizeBytes = 200 * MB
	}

	// 向上取整计算分片数量
	if fileSizeBytes%partSizeBytes == 0 {
		parts = fileSizeBytes / partSizeBytes
	} else {
		parts = fileSizeBytes/partSizeBytes + 1
	}

	return
}

func DownloadToFile(filename, url string, headers map[string]string) {
	if url == "" {
		return
	}

	dir := path.Join(AppConfig.WorkDir, "upload", filename)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		if _, err := DB.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?);INSERT INTO file (name, hash, size, mime, ref_count, created_at) VALUES (?,?,?,?,?,?)`, 1, "0-1", filename, 1, filename, 0, 0, "", time.Now().Unix(), filename, 0, 1, "", 1, time.Now().Unix()); err != nil {
			log.Println(err)
		}
		WriteStringToFile(filename, err.Error())
		return
	}
	req.Header.Set("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36")
	// 设置 Header
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Timeout: 2 * time.Minute,
	}

	resp, err := client.Do(req)
	if err != nil {
		if _, err := DB.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?);INSERT INTO file (name, hash, size, mime, ref_count, created_at) VALUES (?,?,?,?,?,?)`, 1, "0-1", filename, 1, filename, 0, 0, "", time.Now().Unix(), filename, 0, 1, "", 1, time.Now().Unix()); err != nil {
			log.Println(err)
		}
		WriteStringToFile(filename, err.Error())
		return

	}
	defer resp.Body.Close()

	// 状态码校验
	if resp.StatusCode != http.StatusOK {
		if _, err := DB.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?);INSERT INTO file (name, hash, size, mime, ref_count, created_at) VALUES (?,?,?,?,?,?)`, 1, "0-1", filename, 1, filename, 0, 0, "", time.Now().Unix(), filename, 0, 1, "", 1, time.Now().Unix()); err != nil {
			log.Println(err)
		}
		WriteStringToFile(filename, "状态码无效")
		return
	}

	// 创建文件
	out, err := os.Create(dir)
	if err != nil {
		if _, err := DB.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?);INSERT INTO file (name, hash, size, mime, ref_count, created_at) VALUES (?,?,?,?,?,?)`, 1, "0-1", filename, 1, filename, 0, 0, "", time.Now().Unix(), filename, 0, 1, "", 1, time.Now().Unix()); err != nil {
			log.Println(err)
		}
		WriteStringToFile(filename, err.Error())
		return
	}

	// 流式写入文件
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		if _, err := DB.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?);INSERT INTO file (name, hash, size, mime, ref_count, created_at) VALUES (?,?,?,?,?,?)`, 1, "0-1", filename, 1, filename, 0, 0, "", time.Now().Unix(), filename, 0, 1, "", 1, time.Now().Unix()); err != nil {
			log.Println(err)
		}
		out.Close()
		WriteStringToFile(filename, err.Error())
		return
	}
	out.Close()
	size, hash, mime, exp, err := GetFileSizeAndMD5(filename)
	if err != nil {
		if _, err := DB.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?);INSERT INTO file (name, hash, size, mime, ref_count, created_at) VALUES (?,?,?,?,?,?)`, 1, "0-1", filename, size, filename, 0, 0, "", time.Now().Unix(), filename, hash, size, mime, 1, time.Now().Unix()); err != nil {
			log.Println(err)
		}
		WriteStringToFile(filename, err.Error())
		DeleteFile(filename)
		return
	}
	filenames := fmt.Sprintf("%s.%s", filename, exp)
	if _, err := DB.Exec(`INSERT INTO logical (parent_id,path,name,size,file_name,is_dir,is_share,pwd,created_at) values (?,?,?,?,?,?,?,?,?);INSERT INTO file (name, hash, size, mime, ref_count, created_at) VALUES (?,?,?,?,?,?)`, 1, "0-1", filenames, size, filename, 0, 0, "", time.Now().Unix(), filename, hash, size, mime, 1, time.Now().Unix()); err != nil {
		log.Println(err)
		DeleteFile(filename)
	}
}
