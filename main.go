package main

import (
	"ai-web-free/internal/bootstrap"
	"os"

	"github.com/g-utils/endless"
)

func main() {
	app, err := bootstrap.Init()
	if err != nil {
		panic(err)
	}
	defer app.Cleanup()

	host := app.Config.ServerHost
	port := app.Config.ServerPort
	if host == "" {
		host = "0.0.0.0"
	}
	if port == "" {
		port = os.Getenv("PORT")
		if port == "" {
			port = "18080"
		}
	}

	if app.Config.TLSCert != "" && app.Config.TLSKey != "" {
		_ = endless.ListenAndServeTLS(host+":"+port, app.Config.TLSCert, app.Config.TLSKey, app.Router)
	} else {
		_ = endless.ListenAndServe(host+":"+port, app.Router)
	}
}
