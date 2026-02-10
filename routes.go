package main

import (
	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

// SetupRoutes 设置所有路由
func SetupRoutes(r *router.Router) {
	// 页面路由
	r.GET("/login", BlockMiddleware(LoginHandler))
	r.GET("/", BlockMiddleware(PageHandler("index.html")))
	r.GET("/t", BlockMiddleware(PageHandler("test.html")))
	r.GET("/static/{filepath:*}", BlockMiddleware(func(ctx *fasthttp.RequestCtx) {
		StaticHandler(ctx)
	}))
	r.GET("/api/login", BlockMiddleware(LoginAPIHandler))
	r.GET("/api/loginout", AuthCheckMiddleware(LoginOutAPIHandler))

	r.GET("/api/setting/get", AuthCheckMiddleware(GetSettingHandler))
	r.GET("/api/setting/cleanfile", AuthCheckMiddleware(CleanFileHandler))
	r.GET("/api/error/get", AuthCheckMiddleware(GetErrorHandler))
	r.GET("/api/error/clear", AuthCheckMiddleware(ClearErrorHandler))

	r.GET("/api/secret/updata", AuthCheckMiddleware(UpdataSecretHandler))
	r.GET("/api/auth/updata", AuthCheckMiddleware(UpdataAuthHandler))
	r.POST("/api/setting/update_bkg", AuthCheckMiddleware(UpdataBkgHandler))
	r.GET("/api/setting/clean_lim", AuthCheckMiddleware(CleanLimHandler))
	r.GET("/api/store/get", AuthCheckMiddleware(GetStoreHandler))
	r.GET("/api/filelist/get", AuthCheckMiddleware(GetFileHandler))
	r.GET("/api/file/download", AuthCheckMiddleware(DownloadFileHandler))
	r.GET("/api/file/delete", AuthCheckMiddleware(DeleteFileHandler))
	r.GET("/api/filefolder/create", AuthCheckMiddleware(CreateFolderHandler))
	r.GET("/api/file/create", AuthCheckMiddleware(CreateFileHandler))
	r.GET("/api/file/ready-file", AuthCheckMiddleware(ReadyUploadHandler))
	r.POST("/api/file/upload", AuthCheckMiddleware(UploadFileHandler))
	r.GET("/api/imaging/preview", AuthCheckMiddleware(PreviewImagingHandler))
	r.GET("/api/file/preview", AuthCheckMiddleware(PreviewFileHandler))
	r.POST("/api/txtfile/save", AuthCheckMiddleware(SaveFileHandler))

	r.NotFound = func(ctx *fasthttp.RequestCtx) {
		ctx.Redirect("/", 302)
	}
}
