package main

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/dhowden/tag"
	"github.com/disintegration/imaging"
	"github.com/h2non/filetype"
	"github.com/pquerna/otp/totp"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/valyala/fasthttp"
)

var (
	IpMap     = make(map[string]uint8)
	IpMutex   sync.RWMutex
	IdCounter int64
)

// 布隆过滤器变量
var BloomFilter *bloom.BloomFilter

// 初始化布隆过滤器
func InitBloomFilter() {
	// 创建一个能够容纳10000个元素且误判率为0.01的布隆过滤器
	BloomFilter = bloom.NewWithEstimates(10000, 0.01)
}

// 向布隆过滤器添加元素
func AddToBloomFilter(item string) {
	if BloomFilter != nil {
		BloomFilter.Add([]byte(item))
	}
}

// 棜查布隆过滤器中是否存在元素
func CheckInBloomFilter(item string) bool {
	if BloomFilter == nil {
		return false
	}
	return BloomFilter.Test([]byte(item))
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
func GetDeviceStorageInfo() (free, total uint64, err error) {
	usage, err := disk.Usage("/")
	if err != nil {
		usage, err = disk.Usage("C:")
		if err != nil {
			return 0, 0, err
		}
	}
	return usage.Free, usage.Total, nil
}

// DeleteFile 物理删除文件
func DeleteFile(dir, filename string) {
	paths := filepath.Join(WORKDIR, dir, filename)
	err := os.Remove(paths)
	if err != nil {
		log.Println(err)
	}
}

func MsgResponse(ctx *fasthttp.RequestCtx, code int, msg string) {
	ctx.SetContentType("application/json; charset=utf-test.txt")
	ctx.SetStatusCode(code)
	ctx.WriteString(msg)
}

func Timestamp10ToBJTime(ts int64) string {
	return time.Unix(ts, 0).
		In(time.FixedZone("CST", 8*3600)).
		Format("2006年01月02日 15时04分05秒")
}

func B2s(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func GetFileSizeAndMD5(filepath string) (size int, md5hash, mime string, err error) {
	file, err := os.Open(filepath)
	if err != nil {
		return 0, "", "", err
	}
	defer file.Close()

	hash := md5.New()
	header := make([]byte, 512)

	buffer := make([]byte, 4096)
	totalSize := 0
	for {
		readN, readErr := file.Read(buffer)
		if readN > 0 {
			if totalSize < 512 {
				end := readN
				if totalSize+end > 512 {
					end = 512 - totalSize
				}
				copy(header[totalSize:], buffer[:end])
			}
			totalSize += readN
			if _, err := hash.Write(buffer[:readN]); err != nil {
				return 0, "", "", err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return 0, "", "", readErr
		}
	}

	hashStr := hex.EncodeToString(hash.Sum(nil))

	if kind, err := filetype.Match(header[:minInt(int(totalSize), 512)]); err == nil && kind != filetype.Unknown {
		mime = kind.MIME.Value

		if kind.Extension == "zip" {
			if officeType := detectOfficeFile(filepath); officeType != "" {
				switch officeType {
				case "docx":
					mime = "application/docx"
				case "xlsx":
					mime = "application/xlsx"
				}
			}
		}

	} else {
		mime = "text/plain"
	}

	return totalSize, hashStr, mime, nil
}

// ---- 新增辅助函数 ----
func detectOfficeFile(path string) string {
	r, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "[Content_Types].xml" {
			rc, _ := f.Open()
			buf := make([]byte, f.UncompressedSize64)
			rc.Read(buf)
			content := string(buf)
			if strings.Contains(content, "word/") {
				return "docx"
			}
			if strings.Contains(content, "xl/") {
				return "xlsx"
			}
			if strings.Contains(content, "ppt/") {
				return "pptx"
			}
		}
	}
	return ""
}

func Exists(table, where string, args ...any) bool {
	var dummy int
	err := DB.QueryRow(
		fmt.Sprintf("SELECT 1 FROM %s WHERE %s LIMIT 1", table, where),
		args...,
	).Scan(&dummy)
	return err == nil
}

func NextID() string {
	id := atomic.AddInt64(&IdCounter, 1)
	return strconv.FormatInt(id, 10)
}

func WriteToFile(filename string, data []byte) error {
	err := os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}
	return nil
}

func UpDataQr() error {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "GODISK", // 显示应用名称
		AccountName: "admin",  // 账号名称
	})
	if err != nil {
		return err
	}
	MU.Lock()
	Secret = key.Secret()
	AuthValue = string(GenerateRandomString(12))
	F2aUrl = key.URL()
	MU.Unlock()
	return nil
}

func CreateImaging(musicPath string) {
	ff, err := os.Open(musicPath)
	if err != nil {
		log.Printf("无法打开文件 %s: %v", musicPath, err)
		return
	}
	defer ff.Close()

	m, err := tag.ReadFrom(ff)
	if err != nil {
		log.Printf("解析元数据失败 %s: %v", musicPath, err)
		return
	}

	pic := m.Picture()
	if pic == nil {
		log.Printf("跳过：文件没有内置封面 %s", musicPath)
		return
	}

	img, err := imaging.Decode(bytes.NewReader(pic.Data))
	if err != nil {
		log.Printf("图片解码失败 %s: %v", musicPath, err)
		return
	}

	thumb := imaging.Fill(img, 200, 200, imaging.Center, imaging.Lanczos)
	outputPath := musicPath + ".jpg"
	err = imaging.Save(thumb, outputPath)
	if err != nil {
		log.Printf("保存缩略图失败 %s: %v", musicPath, err)
		return
	}
}
