package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type config struct {
	Service      string
	Port         string
	HealthPath   string
	ExtraHeaders [][2]string
}

func loadConfig(getenv func(string) string) config {
	c := config{
		Service:    getenv("SERVICE_NAME"),
		Port:       getenv("PORT"),
		HealthPath: getenv("HEALTH_PATH"),
	}
	if c.Service == "" {
		c.Service = "mock"
	}
	if c.Port == "" {
		c.Port = "3000"
	}
	if c.HealthPath == "" {
		c.HealthPath = "/healthz"
	}
	c.ExtraHeaders = parseExtraHeaders(getenv("EXTRA_RESPONSE_HEADERS"))
	return c
}

func parseExtraHeaders(s string) [][2]string {
	var out [][2]string
	for _, part := range strings.Split(s, ";") {
		name, value, ok := strings.Cut(part, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		out = append(out, [2]string{name, strings.TrimSpace(value)})
	}
	return out
}

func main() {
	cfg := loadConfig(os.Getenv)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           newMock(cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	log.Printf("mock %q listening on :%s (health %s)", cfg.Service, cfg.Port, cfg.HealthPath)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
