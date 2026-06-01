package internalhttp

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ivvklimov/image-previewer/internal/logger"
)

// Обертка для захвата статуса и размера ответа.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// LoggingMiddleware - мидлвара, логирующая запросы в формате:
// 66.249.65.3 [25/Feb/2020:19:11:24 +0600] GET /hello?q=1 HTTP/1.1 200 30 "Mozilla/5.0".
func LoggingMiddleware(log *logger.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Оборачиваем ResponseWriter для захвата метрик
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Выполняем следующий хэндлер
		next.ServeHTTP(wrapped, r)

		// Формируем лог в требуемом формате
		ip := getClientIP(r)
		timestamp := start.Format("02/Jan/2006:15:04:05 -0700")
		method := r.Method
		path := r.URL.RequestURI()
		proto := r.Proto
		status := wrapped.statusCode
		size := wrapped.bytesWritten
		latency := time.Since(start).Milliseconds()
		ua := r.UserAgent()
		if ua == "" {
			ua = "-"
		}

		logLine := fmt.Sprintf("%s [%s] %s %s %s %d %d %d \"%s\"",
			ip, timestamp, method, path, proto, status, size, latency, ua)

		log.Info(logLine)
	})
}

// Извлекает реальный IP клиента (учитывает прокси/балансировщики).
func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, err := net.SplitHostPort(xff); err == nil {
			return ip
		}
		for _, ip := range splitComma(xff) {
			if ip := trimSpace(ip); ip != "" {
				return ip
			}
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return trimSpace(xri)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func splitComma(s string) []string {
	var res []string
	start := 0
	for i, c := range s {
		if c == ',' {
			res = append(res, s[start:i])
			start = i + 1
		}
	}
	res = append(res, s[start:])
	return res
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
