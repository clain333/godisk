package main

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

// init函数在main函数之前执行
func init() {
	// 生成cookie key和54位随机字符串作为cookie值
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	rand.Seed(time.Now().UnixNano())
	AuthValue = string(GenerateRandomString(54))
	InitBloomFilter()
	InitConfig("pwd/config.yaml")
	InitDB()
	InitidCounter()
	InitStaticFS()
	InitViewFS()
	go InitdoWatchdogTask()
	go InitCleanLim()
	go InitCleanFile()

}

func main() {
	defer CloseDB()
	r := router.New()
	SetupRoutes(r)
	server := &fasthttp.Server{
		Handler:            r.Handler,
		Name:               "GOdisk",
		MaxRequestBodySize: 201 * 1024 * 1024,
		WriteTimeout:       30 * time.Second, // 限制服务器响应超时
		IdleTimeout:        60 * time.Second, // 空闲连接超时
		MaxConnsPerIP:      20,
		DisableKeepalive:   false,
		ReduceMemoryUsage:  true,
	}
	addr := fmt.Sprintf(":%d", AppConfig.Port)
	log.Printf("Starting server on port %d...\n", AppConfig.Port)
	log.Fatal(server.ListenAndServe(addr))
}
