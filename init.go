package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"path"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/valyala/fasthttp"
	"gopkg.in/yaml.v3"
)

func InitidCounter() {
	if err := DB.QueryRow("SELECT name FROM file ORDER BY name DESC LIMIT 1;").Scan(&IdCounter); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			IdCounter = 436972
			return
		}
		log.Fatal(err)
	}
}

func InitDB() {
	dbPath := path.Join(AppConfig.WorkDir, AppConfig.SqliteName)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Println(err)
	}
	if err := db.Ping(); err != nil {
		log.Println(err)
	}
	if _, err := db.Exec(CreateSql, time.Now().Unix()); err != nil {
		if err.Error() != "UNIQUE constraint failed: logical.id" {
			log.Println(err)
		}

	}
	log.Println("SQLite 数据库初始化成功！")

	DB = db
}

// LoadConfig 从指定路径加载配置文件
func InitConfig(path string) {
	// 读取配置文件
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Println(err)
		return
	}

	// 解析YAML
	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Println(err)
		return
	}
	AppConfig = &config
	log.Println("config启动成功")
	return
}
func InitdoWatchdogTask() {
	ticker := time.NewTicker(5 * time.Minute) // 每两分钟触发一次
	defer ticker.Stop()

	for {
		<-ticker.C
		var deleteslice = []string{}
		var liveslice = []string{}
		UploadMapMu.RLock()
		for _, v := range UploadMap {
			if !v.IsLive {

				go DeleteFile(v.Code)

				deleteslice = append(deleteslice, v.Code)
			} else {
				liveslice = append(liveslice, v.Code)

			}
		}
		UploadMapMu.RUnlock()
		UploadMapMu.Lock()
		for _, v := range deleteslice {
			delete(UploadMap, v)
		}
		for _, v := range liveslice {
			UploadMap[v].IsLive = false
		}
		UploadMapMu.Unlock()
	}
}

func InitCleanLim() {
	ticker := time.NewTicker(time.Hour * 24)
	defer ticker.Stop()

	for {
		<-ticker.C
		LimitingMap = sync.Map{}
	}
}

func InitCleanFile() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		tx, err := DB.Begin()
		if err != nil {
			log.Println(err)
			continue
		}

		rows, err := tx.Query(`SELECT name FROM file WHERE ref_count <= 0;`)
		if err != nil {
			log.Println(err)
			tx.Rollback()
			continue
		}

		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				log.Println(err)
				rows.Close()
				tx.Rollback()
				continue
			}
			names = append(names, name)
		}
		rows.Close()

		if len(names) == 0 {
			tx.Rollback()
			continue
		}

		// 动态生成占位符
		placeholders := make([]string, len(names))
		args := make([]interface{}, len(names))
		for i, v := range names {
			placeholders[i] = "?"
			args[i] = v
		}

		sql := fmt.Sprintf(`DELETE FROM file WHERE name IN (%s)`, strings.Join(placeholders, ","))
		if _, err := tx.Exec(sql, args...); err != nil {
			log.Println(err)
			tx.Rollback()
			continue
		}

		// 先提交事务
		if err := tx.Commit(); err != nil {
			log.Println(err)
			continue
		}

		// 再删除文件
		for _, v := range names {
			DeleteFile(v)
		}
	}
}

var (
	StaticHandler fasthttp.RequestHandler
	ViewHandler   fasthttp.RequestHandler
)

func InitStaticFS() {
	dir := path.Join(AppConfig.WorkDir)
	fs := &fasthttp.FS{
		Root:               dir,
		IndexNames:         nil,
		GenerateIndexPages: false,
		Compress:           true,
	}
	StaticHandler = fs.NewRequestHandler()
}
func InitViewFS() {
	dir := path.Join(AppConfig.WorkDir)
	fs := &fasthttp.FS{
		Root:               dir,
		IndexNames:         nil,
		GenerateIndexPages: false,
		Compress:           false,
		AcceptByteRange:    true,
	}
	ViewHandler = fs.NewRequestHandler()
}
