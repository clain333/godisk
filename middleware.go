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
			MsgResponse(ctx, FAIL, "ip 被封禁")
			return
		}
		next(ctx)
	}
}

// AuthCheckMiddleware 认证检查中间件
func AuthCheckMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		authValue := ctx.Request.Header.Cookie(AuthKey)
		s := *(*string)(unsafe.Pointer(&authValue))
		if IsAuth {
			if s != AuthValue {
				MsgResponse(ctx, FAIL, "token 错误")
				return
			}
		}

		next(ctx)
	}
}

func RateLimitingMiddleware(next fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		select {
		case <-LeakyBucket:
			next(ctx)
		default:
			MsgResponse(ctx, FAIL, "限流保护")
			return
		}
	}
}
