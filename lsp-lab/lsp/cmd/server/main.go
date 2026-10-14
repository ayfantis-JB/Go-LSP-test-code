// Command server is a long-running HTTP service; used by the "app" container.
//
//	curl localhost:8080/shapes
//	curl -X POST 'localhost:8080/circle?r=2'
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"learn_go_by_example/lsp/shapes"
	"learn_go_by_example/lsp/store"
)

type server struct {
	shapes *store.Store[int, shapes.Shape]
	nextID int
}

func (s *server) list(w http.ResponseWriter, _ *http.Request) {
	out := map[string]any{"total_area": 0.0, "items": []string{}}
	var all []shapes.Shape
	for _, id := range s.shapes.Keys() {
		sh, _ := s.shapes.Get(id)
		all = append(all, sh)
	}
	out["items"] = store.Map(all, shapes.Describe)
	out["total_area"] = shapes.TotalArea(all...)
	writeJSON(w, out)
}

func (s *server) addCircle(w http.ResponseWriter, r *http.Request) {
	radius, err := strconv.ParseFloat(r.URL.Query().Get("r"), 64)
	if err != nil {
		http.Error(w, "query param r must be a number", http.StatusBadRequest)
		return
	}
	s.nextID++
	s.shapes.Put(s.nextID, shapes.Circle{Radius: radius})
	writeJSON(w, map[string]int{"id": s.nextID})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode: %v", err)
	}
}

func main() {
	addr := ":8080"
	if v := os.Getenv("ADDR"); v != "" {
		addr = v
	}

	s := &server{shapes: store.New[int, shapes.Shape]()}
	s.shapes.Put(0, shapes.NewSquare(1))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /shapes", s.list)
	mux.HandleFunc("POST /circle", s.addCircle)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Print("shut down")
}
