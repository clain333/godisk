package main

import (
	"log"
	"math/rand"
	"time"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

// init函数在main函数之前执行
func init() {
	// 生成cookie key和54位随机字符串作为cookie值
	InitEnv()
	InitLog()
	go InitRateLimiting()
	rand.Seed(time.Now().UnixNano())
	InitBloomFilter()
	InitDB()
	InitidCounter()
	InitStaticFS()
	if err := UpDataQr(); err != nil {
		log.Fatal(err)
	}
	go InitdoWatchdogTaskDelfile()
}

func main() {
	defer CloseDB()
	defer CloseLog()
	r := router.New()
	SetupRoutes(r)
	server := &fasthttp.Server{
		Handler:            RateLimitingMiddleware(r.Handler),
		Name:               "GODISK",
		MaxRequestBodySize: MaxUploadSize * 1024 * 1024 * 1024,
		WriteTimeout:       20 * time.Minute,
		ReadTimeout:        20 * time.Minute,
		IdleTimeout:        time.Minute,
		MaxConnsPerIP:      30,
		Concurrency:        500,
		StreamRequestBody:  true,
		ReduceMemoryUsage:  true,
	}

	log.Printf("Starting server on port %s...\n", Port)
	log.Fatal(server.ListenAndServe(":" + Port))

}
