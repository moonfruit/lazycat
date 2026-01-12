package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed web.html icon.png
var webContent embed.FS

var (
	logBus *LogBroadcaster
)

type LogMessage struct {
	Timestamp string `json:"timestamp"`
	Message   string `json:"message"`
	Level     string `json:"level"`
}

type LogBroadcaster struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan LogMessage
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.RWMutex
	history    []LogMessage
	maxHistory int
}

func NewLogBroadcaster(maxHistory int) *LogBroadcaster {
	return &LogBroadcaster{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan LogMessage, 100),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		history:    make([]LogMessage, 0, maxHistory),
		maxHistory: maxHistory,
	}
}

func (lb *LogBroadcaster) run() {
	for {
		select {
		case client := <-lb.register:
			lb.mu.Lock()
			lb.clients[client] = true
			lb.mu.Unlock()

			// 发送历史日志
			lb.mu.RLock()
			for _, msg := range lb.history {
				if err := client.WriteJSON(msg); err != nil {
					client.Close()
					delete(lb.clients, client)
				}
			}
			lb.mu.RUnlock()

		case client := <-lb.unregister:
			lb.mu.Lock()
			if _, ok := lb.clients[client]; ok {
				delete(lb.clients, client)
				client.Close()
			}
			lb.mu.Unlock()

		case message := <-lb.broadcast:
			// 添加到历史记录
			lb.mu.Lock()
			lb.history = append(lb.history, message)
			if len(lb.history) > lb.maxHistory {
				lb.history = lb.history[1:]
			}
			lb.mu.Unlock()

			// 广播给所有客户端
			lb.mu.RLock()
			for client := range lb.clients {
				if err := client.WriteJSON(message); err != nil {
					go func(c *websocket.Conn) {
						lb.unregister <- c
					}(client)
				}
			}
			lb.mu.RUnlock()
		}
	}
}

func (lb *LogBroadcaster) Log(level, format string, v ...any) {
	msg := LogMessage{
		Timestamp: time.Now().Format("2006-01-02 15:04:05.000"),
		Message:   fmt.Sprintf(format, v...),
		Level:     level,
	}

	// 同时输出到标准日志
	log.Printf("[%s] %s", level, msg.Message)

	// 广播到 WebSocket 客户端
	select {
	case lb.broadcast <- msg:
	default:
		// 如果通道满了,跳过
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	logBus.register <- conn

	// 保持连接直到客户端断开
	defer func() {
		logBus.unregister <- conn
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func handleWeb(w http.ResponseWriter, r *http.Request) {
	content, err := webContent.ReadFile("web.html")
	if err != nil {
		http.Error(w, "Failed to load web page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

func handleAPI(w http.ResponseWriter, r *http.Request) {
	logBus.mu.RLock()
	history := logBus.history
	logBus.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"logs": history,
	})
}

func handleFavicon(w http.ResponseWriter, _ *http.Request) {
	content, err := webContent.ReadFile("icon.png")
	if err != nil {
		http.Error(w, "Favicon not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(content)
}

func main() {
	tcpPort := flag.String("port", "1234", "TCP server port")
	flag.StringVar(tcpPort, "p", "1234", "TCP server port (shorthand)")
	webPort := flag.String("web-port", "8080", "Web server port")
	flag.StringVar(webPort, "w", "8080", "Web server port (shorthand)")
	flag.Parse()

	// 初始化日志广播器
	logBus = NewLogBroadcaster(1000)
	go logBus.run()

	// 启动 HTTP 服务器
	http.HandleFunc("/", handleWeb)
	http.HandleFunc("/ws", handleWebSocket)
	http.HandleFunc("/api/logs", handleAPI)
	http.HandleFunc("/favicon.ico", handleFavicon)

	go func() {
		logBus.Log("INFO", "Web server starting on port %s", *webPort)
		if err := http.ListenAndServe(":"+*webPort, nil); err != nil {
			log.Fatalf("Failed to start web server: %v", err)
		}
	}()

	// 启动 TCP 服务器
	listener, err := net.Listen("tcp", ":"+*tcpPort)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", *tcpPort, err)
	}
	defer listener.Close()

	logBus.Log("INFO", "TCP server listening on port %s", *tcpPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			logBus.Log("ERROR", "Failed to accept connection: %v", err)
			continue
		}

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	remoteAddr := conn.RemoteAddr().String()

	logBus.Log("INFO", "[%s] Connection established", remoteAddr)
	defer func() {
		conn.Close()
		logBus.Log("INFO", "[%s] Connection closed", remoteAddr)
	}()

	reader := bufio.NewReader(conn)
	buf := make([]byte, 4096)

	for {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				logBus.Log("ERROR", "[%s] Read error: %v", remoteAddr, err)
			}
			return
		}

		if n > 0 {
			logBus.Log("DATA", "[%s] [%04d] <<< %s", remoteAddr, n, string(buf[:n]))
		}
	}
}
