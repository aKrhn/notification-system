package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/karahan/notification-system/internal/pubsub"
)

type WebSocketHandler struct {
	ps *pubsub.PubSub
}

func NewWebSocketHandler(ps *pubsub.PubSub) *WebSocketHandler {
	return &WebSocketHandler{ps: ps}
}

func (h *WebSocketHandler) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		slog.Error("websocket: failed to accept", "error", err)
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()

	sub := h.ps.Subscribe(ctx)
	defer sub.Close()

	ch := sub.Channel()

	slog.Info("websocket: client connected", "remote", r.RemoteAddr)

	for {
		select {
		case <-ctx.Done():
			conn.Close(websocket.StatusNormalClosure, "server shutting down")
			slog.Info("websocket: client disconnected (context)", "remote", r.RemoteAddr)
			return
		case msg, ok := <-ch:
			if !ok {
				conn.Close(websocket.StatusNormalClosure, "subscription closed")
				return
			}

			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := wsjson.Write(writeCtx, conn, json.RawMessage(msg.Payload))
			cancel()

			if err != nil {
				slog.Debug("websocket: write failed, closing", "error", err)
				return
			}
		}
	}
}
