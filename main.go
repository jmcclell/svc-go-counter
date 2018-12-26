package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"time"

	"github.com/go-redis/redis"
	"github.com/gorilla/schema"
	"github.com/heptiolabs/healthcheck"
	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var paramDecoder = schema.NewDecoder()
var config Config
var rclient *redis.Client
var metrics = prometheus.NewRegistry()
var health = healthcheck.NewMetricsHandler(metrics, "counter")
var serverStatus = Starting
var version = "dev"

const BaseLabel = "next"

type Config struct {
	Port                                int16         `default:"80"`
	AdminPort                           int16         `default:"9000"`
	GracefulShutdownTimeout             time.Duration `default:"30s"`
	RedisURL                            string        `split_words:"true" default:"localhost:6379"`
	RedisPW                             string        `split_words:"true" default:""`
	RedisDB                             int           `split_words:"true" default:"0"`
	RedisPrefix                         string        `split_words:"true" default:"counter"`
	RedisHealthyConnectTimeoutThreshold time.Duration `default:"100ms"`
}

func main() {
	shutdown := make(chan os.Signal)
	signal.Notify(shutdown, os.Interrupt)

	initConfig()
	initAdminServer()
	initRedisClient()

	router := http.NewServeMux()
	router.HandleFunc("/", counterHandler)

	server := &http.Server{
		Handler: router,
	}

	log.Printf("Starting HTTP on 0.0.0.0:%d", config.Port)
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", config.Port))
	if err != nil {
		log.Fatal(err)
	}

	go server.Serve(listener)
	log.Println("Ready to serve requests")
	serverStatus = Running

	<-shutdown

	serverStatus = ShuttingDown
	log.Println("Shutting down...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), config.GracefulShutdownTimeout)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatal(err)
	}

	log.Println("Graceful shutdown complete.")
}

func initConfig() {
	err := envconfig.Process("", &config)
	if err != nil {
		log.Fatal(err)
	}
}

func initAdminServer() {
	initHealthcheck()

	adminRouter := http.NewServeMux()
	adminRouter.Handle("/metrics", promhttp.HandlerFor(metrics, promhttp.HandlerOpts{}))
	adminRouter.HandleFunc("/live", health.LiveEndpoint)
	adminRouter.HandleFunc("/ready", health.ReadyEndpoint)
	adminRouter.HandleFunc("/about", aboutHandler)

	adminServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.AdminPort),
		Handler: adminRouter,
	}

	log.Printf("Starting admin server on 0.0.0.0:%d", config.AdminPort)
	go func() {
		err := adminServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Println(err.Error())
		}
	}()
}

func aboutHandler(w http.ResponseWriter, r *http.Request) {
	hostname, _ := os.Hostname()
	response := AboutResponse{Name: "counter", Version: version, Hostname: hostname}
	response.render(w)
}

func initHealthcheck() {
	health.AddReadinessCheck("http", func() error {
		if serverStatus == Running {
			return nil
		} else {
			return fmt.Errorf("HTTP server is %s", serverStatus)
		}
	})

	health.AddReadinessCheck("redis",
		healthcheck.Async(
			healthcheck.TCPDialCheck(config.RedisURL, config.RedisHealthyConnectTimeoutThreshold), 10*time.Second))
}

func initRedisClient() {
	rclient = redis.NewClient(&redis.Options{
		Addr:     config.RedisURL,
		Password: config.RedisPW,
		DB:       config.RedisDB,
	})
}

func counterHandler(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		ErrorResponse{Error: err.Error()}.render(w, http.StatusBadRequest)
		return
	}

	params := CounterRequest{Label: "default"}

	err = paramDecoder.Decode(&params, r.Form)
	if err != nil {
		ErrorResponse{Error: err.Error()}.render(w, http.StatusBadRequest)
		return
	}

	valid, err := regexp.Match("[a-zA-Z0-9]+", []byte(params.Label))
	if !valid || err != nil {
		ErrorResponse{Error: "invalid label"}.render(w, http.StatusBadRequest)
		return
	}

	val, err := rclient.Incr(getKey(params.Label)).Result()
	if err != nil {
		ErrorResponse{Error: err.Error()}.render(w, http.StatusBadRequest)
		return
	}

	NumberResponse{Value: val}.render(w)
}

func getKey(label string) string {
	return fmt.Sprintf("%s.%s.%s", config.RedisPrefix, BaseLabel, label)
}

type CounterRequest struct {
	Label string `schema:"label"`
}

type NumberResponse struct {
	Value int64 `json:"value"`
}

func (v NumberResponse) render(w http.ResponseWriter) {
	encoded, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(encoded)
}

type AboutResponse struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Hostname string `json:"hostname"`
}

func (a AboutResponse) render(w http.ResponseWriter) {
	encoded, err := json.Marshal(a)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(encoded)
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func (e ErrorResponse) render(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "application/json")

	encoded, err := json.Marshal(e)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{ "error": "%s" }`, err.Error()), http.StatusInternalServerError)
	}

	http.Error(w, string(encoded), code)
}

type ServerStatus int

const (
	Starting ServerStatus = iota
	Running
	ShuttingDown
)

func (s ServerStatus) String() string {
	return [...]string{"starting", "running", "shutting down"}[s]
}
