package main

import (
	"sync"

	"github.com/valyala/fasthttp"
)

var (
	AuthValue    string
	MU           sync.RWMutex
	UploadMap    = make(map[string]*FileContext)
	UploadMapMu  sync.RWMutex
	LimitingMap  = sync.Map{}
	JsonRespPool = sync.Pool{
		New: func() any {
			return new(JSONResult)
		},
	}
	BufPool = sync.Pool{
		New: func() any {
			return make([]byte, 0, 1024)
		},
	}

	Resp1Pool = sync.Pool{
		New: func() any {
			return new(Resp1)
		},
	}
	Resp2Pool = sync.Pool{
		New: func() any {
			return make([]Resp2, 0)
		},
	}
)

type Config struct {
	Port          int    `yaml:"port"`
	WorkDir       string `yaml:"workdir"`
	Password      string `yaml:"password"`
	MaxShareSize  uint64 `yaml:"max_share_size"`  // 单位 MB
	MaxUploadSize uint64 `yaml:"max_upload_size"` // 单位 GB
	DeadlineStore uint64 `yaml:"deadline_store"`
	SqliteName    string `yaml:"sqlite_name"`
}

type JSONResult struct {
	Message string      `json:"message"` // 提示信息
	Data    interface{} `json:"data"`    // 业务数据
}

type Resp1 struct {
	ParentId uint64     `json:"parent_id"`
	F        []FileInfo `json:"file"`
}
type FileInfo struct {
	ID        uint64 `json:"id"`
	Name      string `json:"name"`
	Size      uint64 `json:"size"`
	IsDir     bool   `json:"is_dir"`
	IsShare   bool   `json:"is_share"`
	CreatedAt string `json:"created_at"`
}
type Resp2 struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

type FileContext struct {
	Code         string
	Name         string
	ParentId     string
	FileSize     uint64
	FileHash     string
	IsLive       bool
	ChunkCount   uint64
	MaxChunkSize uint64
	UploadCount  []uint8
}

const (
	MB        = 1024 * 1024
	CreateSql = `
CREATE TABLE IF NOT EXISTS note (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title VARCHAR(255) NOT NULL UNIQUE,
    text TEXT NOT NULL,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS file (
    name VARCHAR(255) NOT NULL,
    hash VARCHAR(64) NOT NULL,
    size INTEGER NOT NULL DEFAULT 0,
    mime VARCHAR(255) NOT NULL,
    ref_count INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS logical (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    parent_id INTEGER NOT NULL,
    path VARCHAR(512) NOT NULL,
    name VARCHAR(255) NOT NULL,
	pwd VARCHAR(64)    NOT NULL,
    size INTEGER,
    file_name VARCHAR(255) NOT NULL,
    is_dir BOOLEAN NOT NULL,
    is_share BOOLEAN NOT NULL,
    created_at INTEGER NOT NULL
);
INSERT INTO logical (id,parent_id, path, name, size, file_name, is_dir, is_share, pwd,created_at) VALUES (1,0,0,0,0,0,1,0,0,?)
`
	SUCCESSCODE = fasthttp.StatusOK
	FAIL        = 499
	ERROR       = fasthttp.StatusInternalServerError
)
