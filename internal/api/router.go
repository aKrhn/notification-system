package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	httpSwagger "github.com/swaggo/http-swagger"

	_ "github.com/karahan/notification-system/docs"
	"github.com/karahan/notification-system/internal/api/handler"
	"github.com/karahan/notification-system/internal/api/middleware"
)

func NewRouter(nh *handler.NotificationHandler, hh *handler.HealthHandler, mh *handler.MetricsHandler, wsh *handler.WebSocketHandler, th *handler.TemplateHandler) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Correlation)
	r.Use(middleware.Logging)

	r.Get("/health", hh.Health)
	r.Get("/metrics", mh.Metrics)
	r.Get("/swagger/*", httpSwagger.WrapHandler)
	r.Get("/api/v1/ws", wsh.HandleWS)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(contentTypeJSON)

		r.Post("/notifications", nh.Create)
		r.Post("/notifications/batch", nh.CreateBatch)
		r.Get("/notifications", nh.List)
		r.Get("/notifications/{id}", nh.GetByID)
		r.Get("/notifications/batch/{id}", nh.GetBatchStatus)
		r.Patch("/notifications/{id}/cancel", nh.Cancel)

		r.Post("/templates", th.Create)
		r.Get("/templates", th.List)
		r.Get("/templates/{id}", th.GetByID)
	})

	return r
}

func contentTypeJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			ct := r.Header.Get("Content-Type")
			if !strings.Contains(ct, "application/json") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnsupportedMediaType)
				json.NewEncoder(w).Encode(map[string]string{"error": "Content-Type must be application/json"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
