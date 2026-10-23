package openapi

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestResetConnectionOnError(t *testing.T) {
	for _, reset := range []bool{true, false} {
		t.Run(fmt.Sprintf("reset=%v", reset), func(t *testing.T) {
			var mu sync.Mutex
			newConns := 0
			requests := 0

			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.ReadAll(r.Body)
				mu.Lock()
				requests++
				n := requests
				mu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				if n == 2 {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"results":[]}`))
			}))
			srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					mu.Lock()
					newConns++
					mu.Unlock()
				}
			}
			srv.Start()
			defer srv.Close()

			opts := DefaultOptions()
			opts.ResetOnError = reset
			client := NewClient(srv.URL, "key", opts)

			ctx := context.Background()
			if _, err := client.Search(ctx, SearchRequest{Query: "q"}); err != nil {
				t.Fatalf("第一次调用失败: %v", err)
			}
			if _, err := client.Search(ctx, SearchRequest{Query: "q"}); err == nil {
				t.Fatal("第二次调用应失败")
			}
			if _, err := client.Search(ctx, SearchRequest{Query: "q"}); err != nil {
				t.Fatalf("第三次调用失败: %v", err)
			}

			mu.Lock()
			got := newConns
			mu.Unlock()
			want := 1
			if reset {
				want = 2
			}
			if got != want {
				t.Fatalf("新建连接数不符: got %d, want %d", got, want)
			}
		})
	}
}
