package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	log "github.com/sirupsen/logrus"
)

type EnvType string

const DEV EnvType = "dev"
const PROD EnvType = "prod"
const QA EnvType = "qa"

var validEnvTypes = []EnvType{
	DEV,
	PROD,
}

func ValidEnv(s EnvType) bool {
	for _, k := range validEnvTypes {
		if k == s {
			return true
		}
	}
	return false
}

type Littr struct {
	Env         EnvType
	HostName    string
	Port        int64
	Listen      string
	Db          *sql.DB
	SessionKeys [2][]byte
}

func (l *Littr) listen() string {
	if len(l.Listen) > 0 {
		return l.Listen
	}
	var port string
	if l.Port != 0 {
		port = fmt.Sprintf(":%d", l.Port)
	}
	return fmt.Sprintf("%s%s", l.HostName, port)
}

func (l *Littr) BaseUrl() string {
	return fmt.Sprintf("http://%s", l.HostName)
}

func (l *Littr) Run(m http.Handler, wait time.Duration) {
	log.WithFields(log.Fields{}).Infof("starting debug level %q", log.GetLevel().String())
	log.WithFields(log.Fields{}).Infof("listening on %s", l.listen())

	srv := &http.Server{
		Addr: l.listen(),
		// Good practice to set timeouts to avoid Slowloris attacks.
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      m,
	}

	// Run our server in a goroutine so that it doesn't block.
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			log.WithFields(log.Fields{}).Error(err)
			os.Exit(1)
		}
	}()

	c := make(chan os.Signal, 1)
	// We'll accept graceful shutdowns when quit via SIGINT (Ctrl+C)
	// SIGKILL, SIGQUIT or SIGTERM (Ctrl+/) will not be caught.
	signal.Notify(c, os.Interrupt)
	// Block until we receive our signal.
	<-c

	// Create a deadline to wait for.
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	log.RegisterExitHandler(cancel)
	defer cancel()
	// Doesn't block if no connections, but will otherwise wait
	// until the timeout deadline.
	srv.Shutdown(ctx)
	// Optionally, you could run srv.Shutdown in a goroutine and block on
	// <-ctx.Done() if your application should wait for other services
	// to finalize based on context cancellation.
	log.WithFields(log.Fields{}).Infof("shutting down")
	os.Exit(0)
}
