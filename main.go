package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"ganigeorgiev.com/pocketbase/pocketbase"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/plugins/jsvm"
	"ganigeorgiev.com/pocketbase/pocketbase/plugins/migratecmd"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/osutils"

	"miform/internal/api"
	"miform/internal/service"
)

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("[warn] No .env file found (expected in prod):", err)
	}

	isGoRun := osutils.IsProbablyGoRun()

	app := pocketbase.New()

	submissionService := service.New(app)
	apiHandler := api.NewHandler(submissionService)

	app.OnServe().BindFunc(func(se *core.ServeEvent) error {
		// Public form submission endpoints
		se.Router.POST("/submit", apiHandler.Submit)
		se.Router.POST("/submit/{id}", apiHandler.Submit)
		se.Router.GET("/submit/success", apiHandler.Success)

		return se.Next()
	})

	// Parity hook for submissions created directly through PocketBase REST API
	app.OnRecordAfterCreateSuccess("submissions").BindFunc(func(e *core.RecordEvent) error {
		return submissionService.ProcessRecord(e.Record)
	})

	jsvm.MustRegister(app, jsvm.Config{
		HooksWatch:    true,
		MigrationsDir: "pb_migrations",
		HooksPoolSize: 0,
	})

	migratecmd.MustRegister(app, app.RootCmd, migratecmd.Config{
		TemplateLang: migratecmd.TemplateLangJS,
		Automigrate:  isGoRun,
	})

	if err := app.Start(); err != nil {
		app.Logger().Error("app failed to start", "err", err)
		os.Exit(1)
	}
}
