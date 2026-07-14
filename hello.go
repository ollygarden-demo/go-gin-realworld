package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gothinkster/golang-gin-realworld-example-app/articles"
	"github.com/gothinkster/golang-gin-realworld-example-app/common"
	apptelemetry "github.com/gothinkster/golang-gin-realworld-example-app/internal/telemetry"
	"github.com/gothinkster/golang-gin-realworld-example-app/users"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) {
	users.AutoMigrate()
	db.AutoMigrate(&articles.ArticleModel{})
	db.AutoMigrate(&articles.TagModel{})
	db.AutoMigrate(&articles.FavoriteModel{})
	db.AutoMigrate(&articles.ArticleUserModel{})
	db.AutoMigrate(&articles.CommentModel{})
}

func main() {
	providers, err := apptelemetry.Setup(context.Background(), "configs/otel.yaml")
	if err != nil {
		log.Fatal("failed to set up OpenTelemetry:", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := providers.Shutdown(ctx); err != nil {
			log.Println("failed to shut down OpenTelemetry:", err)
		}
	}()
	if err := apptelemetry.InitializeMetrics(); err != nil {
		log.Fatal("failed to initialize telemetry metrics:", err)
	}

	db := common.Init()
	Migrate(db)
	if err := common.InstrumentDB(db); err != nil {
		log.Fatal("failed to instrument database:", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Println("failed to get sql.DB:", err)
	} else {
		defer sqlDB.Close()
	}

	r := gin.New()
	r.Use(otelgin.Middleware(apptelemetry.ServiceName, otelgin.WithFilter(func(req *http.Request) bool {
		return req.URL.Path != "/api/ping/"
	})))
	r.Use(gin.Logger(), gin.Recovery())

	// Disable automatic redirect for trailing slashes
	// This prevents POST body from being lost during redirects
	r.RedirectTrailingSlash = false

	v1 := r.Group("/api")
	users.UsersRegister(v1.Group("/users"))
	v1.Use(users.AuthMiddleware(false))
	articles.ArticlesAnonymousRegister(v1.Group("/articles"))
	articles.TagsAnonymousRegister(v1.Group("/tags"))
	users.ProfileRetrieveRegister(v1.Group("/profiles"))

	v1.Use(users.AuthMiddleware(true))
	users.UserRegister(v1.Group("/user"))
	users.ProfileRegister(v1.Group("/profiles"))

	articles.ArticlesRegister(v1.Group("/articles"))

	testAuth := r.Group("/api/ping")

	testAuth.GET("/", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	// Get port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	if err := r.Run(":" + port); err != nil {
		log.Fatal("failed to start server:", err)
	}
}
