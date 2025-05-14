package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"sync"
	"time"

	"com.example/pod/config"
	"github.com/gorilla/websocket"
)

// Manager WebSocket连接管理器
type Manager struct {
	config    *config.Config
	conn      *websocket.Conn
	connMutex sync.Mutex
	serverURL string
	handlers  map[string]MessageHandler
}

// MessageHandler 消息处理器接口
type MessageHandler func([]byte) error

// NewManager 创建WebSocket连接管理器
func NewManager(config *config.Config) *Manager {
	serverURL := GenerateServerURL(config.RelayServerHost, config.NodeUID)

	return &Manager{
		config:    config,
		serverURL: serverURL,
		handlers:  make(map[string]MessageHandler),
	}
}

// RegisterHandler 注册消息处理器
func (m *Manager) RegisterHandler(messageType string, handler MessageHandler) {
	m.handlers[messageType] = handler
}

// GetConn 获取当前连接
func (m *Manager) GetConn() *websocket.Conn {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()
	return m.conn
}

// IsConnected 检查是否已连接
func (m *Manager) IsConnected() bool {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()
	return m.conn != nil
}

// MaintainConnection 维护WebSocket连接
func (m *Manager) MaintainConnection(ctx context.Context) {
	const reconnectInterval = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			// 上下文已取消，退出函数
			m.connMutex.Lock()
			if m.conn != nil {
				m.conn.Close()
				m.conn = nil
			}
			m.connMutex.Unlock()
			return
		default:
			// 尝试连接
			if !m.IsConnected() {
				log.Printf("正在连接到 %s...", m.serverURL)

				conn, _, dialErr := websocket.DefaultDialer.Dial(m.serverURL, nil)
				if dialErr != nil {
					log.Printf("连接失败: %v", dialErr)
					log.Printf("将在 %v 后重试", reconnectInterval)
					time.Sleep(reconnectInterval)
					continue
				}

				m.connMutex.Lock()
				m.conn = conn
				m.connMutex.Unlock()

				log.Println("连接成功！")

				// 启动一个goroutine来监听消息
				go m.listenForMessages(ctx)

				// 定期发送ping以保持连接活跃
				go m.keepAlive(ctx)
			}

			// 等待重新连接检查
			time.Sleep(time.Second)

			if !m.IsConnected() {
				log.Printf("连接已断开，将在 %v 后重试", reconnectInterval)
				time.Sleep(reconnectInterval - time.Second) // 减去已经睡眠的1秒
			}
		}
	}
}

// listenForMessages 监听WebSocket消息
func (m *Manager) listenForMessages(ctx context.Context) {
	defer func() {
		m.connMutex.Lock()
		if m.conn != nil {
			m.conn.Close()
			m.conn = nil
		}
		m.connMutex.Unlock()
	}()

	// 设置读取超时
	m.connMutex.Lock()
	if m.conn != nil {
		m.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		m.conn.SetPingHandler(func(string) error {
			// 收到ping时重置超时
			m.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			return nil
		})
	}
	m.connMutex.Unlock()

	// 创建消息接收通道
	messageChan := make(chan []byte)
	errorChan := make(chan error)

	// 启动goroutine来读取消息
	go func() {
		for {
			conn := m.GetConn()
			if conn == nil {
				errorChan <- fmt.Errorf("连接已关闭")
				return
			}

			msgType, message, err := conn.ReadMessage()
			if err != nil {
				errorChan <- err
				return
			}

			if msgType == websocket.TextMessage {
				messageChan <- message
			}

			// 重置读取超时
			m.connMutex.Lock()
			if m.conn != nil {
				m.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			}
			m.connMutex.Unlock()
		}
	}()

	// 主循环，处理消息和上下文取消
	for {
		select {
		case <-ctx.Done():
			// 上下文已取消，退出函数
			log.Println("上下文已取消，停止消息监听")
			return
		case err := <-errorChan:
			// 发生错误，记录并退出
			log.Printf("读取错误: %v", err)
			return
		case message := <-messageChan:
			// 处理收到的消息
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("解析消息失败: %v", err)
				continue
			}

			// 根据消息类型调用相应的处理器
			if typeStr, ok := msg["type"].(string); ok {
				if handler, exists := m.handlers[typeStr]; exists {
					if err := handler(message); err != nil {
						log.Printf("处理消息失败: %v", err)
					}
				} else {
					log.Printf("未注册的消息类型: %s", typeStr)
				}
			}
		}
	}
}

// keepAlive 定期发送ping以保持连接活跃
func (m *Manager) keepAlive(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			conn := m.GetConn()
			if conn == nil {
				return
			}

			// {type: "ping", timestamp: 1719638400}
			err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type": "ping", "timestamp": `+strconv.FormatInt(time.Now().Unix(), 10)+`}`))
			if err != nil {
				log.Printf("发送ping失败: %v", err)
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// SendTextMessage 发送文本消息
func (m *Manager) SendTextMessage(msg interface{}) error {
	m.connMutex.Lock()
	defer m.connMutex.Unlock()

	if m.conn == nil {
		return nil
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return m.conn.WriteMessage(websocket.TextMessage, data)
}

// GenerateServerURL 根据配置生成WebSocket服务器URL
func GenerateServerURL(host, nodePath string) string {
	u := url.URL{Scheme: "ws", Host: host, Path: "/socket/node/" + nodePath}
	return u.String()
}
