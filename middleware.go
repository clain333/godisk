package main

import (
	"unsafe"

	"github.com/valyala/fasthttp"
)

// 存储IP地址的map和读写锁

// BlockMiddleware 阻止访问中间件
func BlockMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		clientIP := ctx.RemoteIP().String()
		if CheckInBloomFilter(clientIP) {
			JSONResponse(ctx, FAIL, "访问被拒绝：您的IP已被封锁", nil)
			return
		}

		next(ctx)
	}
}

// AuthCheckMiddleware 认证检查中间件
func AuthCheckMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		authValue := ctx.Request.Header.Cookie("Authorization")
		s := *(*string)(unsafe.Pointer(&authValue))
		// 简单示例：检查是否为空
		if len(authValue) == 0 {
			JSONResponse(ctx, FAIL, "token不能为空", nil)
			return
		}
		if s != AuthValue {
			JSONResponse(ctx, FAIL, "token错误", nil)
			return
		}
		next(ctx)
	}
}

func TrafficLimitingMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		clientIP := ctx.RemoteIP().String()
		var i int64
		if count, ok := LimitingMap.Load(clientIP); ok {
			i = count.(int64)
			if i > 100*1024*1024 {
				JSONResponse(ctx, FAIL, "下载数据过多", nil)
				return
			}
		} else {
			i = 0
		}
		next(ctx)

		size, ok := ctx.UserValue("size").(int64)
		if ok {
			LimitingMap.Store(clientIP, size+i)
		}
	}
}
