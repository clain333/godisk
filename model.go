package main

import (
	"sync"

	"github.com/valyala/fasthttp"
)

var (
	AuthKey       = string(GenerateRandomString(13))
	AuthValue     = string(GenerateRandomString(12))
	IsAuth        = false
	F2aUrl        = ""
	Secret        = ""
	AdminKey      = ""
	LeakyBucket   = make(chan struct{}, 1000)
	AsyncDel      = make(chan int, 1000)
	MaxUploadSize = 1
	MU            sync.Mutex
	Port          = "80"
	StaticHandler fasthttp.RequestHandler
	BufPool       = sync.Pool{
		New: func() any {
			return make([]byte, 0, 1024)
		},
	}
	RespPool = sync.Pool{
		New: func() any {
			return new(Resp)
		},
	}
)

type JsonResp struct {
	Data interface{} `json:"data"`
}

type Resp struct {
	ParentId   uint64     `json:"parent_id"`
	FolderName string     `json:"folder_name"`
	F          []FileInfo `json:"file"`
}
type FileInfo struct {
	ID        uint64 `json:"id"`
	LName     string `json:"lname"`
	Size      uint64 `json:"size"`
	IsDir     uint8  `json:"is_dir"`
	Mime      string `json:"mime"`
	CreatedAt string `json:"created_at"`
}

const (
	WORKDIR   = "pwd"
	CREATESQL = `
CREATE TABLE IF NOT EXISTS file (
    id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    parent_id INTEGER NOT NULL,
    name INTEGER NOT NULL DEFAULT 0,
	lname TEXT NOT NULL,
    is_dir INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS file_blob (
	name INTEGER  NOT NULL,
    hash TEXT NOT NULL,
    size INTEGER NOT NULL,
    mime TEXT  NOT NULL,
	count INTEGER NOT NULL
);

INSERT INTO file (id,parent_id,name,lname,is_dir,created_at) VALUES (1,1,0,"root",1,?);INSERT INTO file_blob (name,hash,size,mime,count) VALUES (0,0,0,'',1);

CREATE INDEX IF NOT EXISTS one ON file_blob(hash);
CREATE INDEX IF NOT EXISTS two ON file_blob(size);
CREATE INDEX IF NOT EXISTS there ON file_blob(name);
CREATE INDEX IF NOT EXISTS four ON file(parent_id);
`
	ChunkSize = 5 * 1024 * 1024
	OK        = fasthttp.StatusOK
	FAIL      = fasthttp.StatusBadRequest
	ERROR     = fasthttp.StatusInternalServerError
)
