package main

import (
	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

// SetupRoutes 设置所有路由
func SetupRoutes(r *router.Router) {
	// 页面路由
	r.GET("/login", BlockMiddleware(LoginHandler))
	r.GET("/", BlockMiddleware((PageHandler("index.html"))))
	r.GET("/note", BlockMiddleware(PageHandler("note.html")))
	r.GET("/view", BlockMiddleware(PageHandler("view.html")))
	r.GET("/static/{filepath:*}", BlockMiddleware(func(ctx *fasthttp.RequestCtx) {
		StaticHandler(ctx)
	}))
	r.GET("/upload/{filepath:*}", AuthCheckMiddleware(func(ctx *fasthttp.RequestCtx) {
		ViewHandler(ctx)
	}))
	r.POST("/api/login", BlockMiddleware(LoginAPIHandler))
	r.GET("/api/loginout", AuthCheckMiddleware(LoginOutAPIHandler))

	r.GET("/api/storage", AuthCheckMiddleware(StoreinfoHandler))
	r.POST("/api/update-setting", AuthCheckMiddleware(ChangeSettingHandler))

	r.GET("/api/file", AuthCheckMiddleware(GetFileHandler))
	r.POST("/api/file/folder", AuthCheckMiddleware(CreateFolderHandler))

	r.POST("/api/note", AuthCheckMiddleware(CreateNoteHandler))
	r.DELETE("/api/note", AuthCheckMiddleware(DeleteNoteHandler))
	r.GET("/api/note", AuthCheckMiddleware(ViewNoteHandler))

	r.POST("/api/file/ready-upload", AuthCheckMiddleware(ReadyUploadHandler))
	r.POST("/api/file/upload", AuthCheckMiddleware(UploadFileHandler))

	r.POST("/api/share", AuthCheckMiddleware(ShareFileHandler))
	r.GET("/api/share", AuthCheckMiddleware(ViewShareFileHandler))
	r.GET("/api/share/download", TrafficLimitingMiddleware(BlockMiddleware(DownloadShareHandler)))

	r.GET("/api/file/download", AuthCheckMiddleware(DownloadFileHandler))

	r.POST("/api/offline_download", AuthCheckMiddleware(OffloneDowloadHandler))

	r.DELETE("/api/file", AuthCheckMiddleware(DeleteFileHandler))
	r.GET("/api/file/preview", AuthCheckMiddleware(PreviewFileHandler))
	r.GET("/api/file/previewinfo", AuthCheckMiddleware(PreviewFileHandHandler))

	r.NotFound = func(ctx *fasthttp.RequestCtx) {
		ctx.Redirect("/", 302)
	}
}
