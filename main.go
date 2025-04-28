package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

type MessageType string

const (
	Pong            MessageType = "pong"
	InitEnv         MessageType = "init_env"
	UpdateProject   MessageType = "update_project"   // 全量更新项目
	UpdateResources MessageType = "update_resources" // 增量更新资源
)

func init() {
	// Load the .env file
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file:", err)
	}
}

func main() {
	// 设置日志
	log.SetOutput(os.Stdout)
	log.SetPrefix("[WebSocket Client] ")

	// 设置命令行参数
	cmdNodeUID := flag.String("node-uid", "", "指定 Node UID")
	flag.Parse()

	// 从命令行获取 nodeUID (优先级高)
	var nodeUID string
	if *cmdNodeUID != "" {
		nodeUID = *cmdNodeUID
		log.Printf("使用命令行参数指定的 Node UID: %s", nodeUID)
	} else {
		// 从环境变量获取nodeID
		nodeUID = os.Getenv("NODE_UID")
		if nodeUID != "" {
			log.Printf("使用环境变量中的 Node UID: %s", nodeUID)
		} else {
			// 既没有命令行参数也没有环境变量
			log.Println("错误: 未指定 Node UID，请通过命令行参数 --node-uid 或环境变量 NODE_UID 设置")
			os.Exit(1)
		}
	}

	// 创建连接的URL
	u := url.URL{Scheme: "ws", Host: "localhost:8080", Path: "/socket/node/" + nodeUID}
	log.Printf("连接到 %s", u.String())

	// 创建一个上下文，用于处理程序终止
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 处理Ctrl+C信号
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// 启动一个goroutine来处理重连逻辑
	go maintainConnection(ctx, u.String())

	// 等待中断信号
	<-interrupt
	log.Println("接收到中断信号，正在关闭...")
	cancel()
	log.Println("程序已关闭")
}

type TextMessage struct {
	Type MessageType `json:"type"`
	Data *any        `json:"data"`
}

func handleTextMessage(message []byte) {
	var textMessage TextMessage
	err := json.Unmarshal(message, &textMessage)
	if err != nil {
		log.Printf("解析消息失败: %v", err)
		return
	}
	switch textMessage.Type {
	case InitEnv:
		handleInitEnv(textMessage.Data)
	case UpdateProject:
		handleUpdateProject(textMessage.Data)
	case UpdateResources:
		handleUpdateResource(textMessage.Data)
	}

}

func handleInitEnv(data *any) {
	log.Printf("收到init_env消息: %v", data)
	// 1. 检查解压软件是否安装
	// 2. 下载压缩文件
	// 3. 解压文件
	// 4. 执行初始化脚本
	// 5. 下载 compose 文件
	// 6. 启动 docker compose
	// 7. 上报启动结果
}

func handleUpdateProject(data *any) {
	log.Printf("收到update_project消息: %v", data)
	// 1. 下载项目
	// 2. 解压项目
	// 3. 执行初始化脚本
	// 4. 上报启动结果
}

func handleUpdateResource(data *any) {
	log.Printf("收到update_resource消息: %v", data)
	// 1. 下载资源
	// 2. 解压资源
	// 3. 执行初始化脚本
	// 4. 上报启动结果
}

func maintainConnection(ctx context.Context, serverURL string) {
	const reconnectInterval = 30 * time.Second
	var conn *websocket.Conn
	var err error

	for {
		select {
		case <-ctx.Done():
			// 上下文已取消，退出函数
			if conn != nil {
				conn.Close()
			}
			return
		default:
			// 尝试连接
			if conn == nil {
				log.Printf("正在连接到 %s...", serverURL)
				conn, _, err = websocket.DefaultDialer.Dial(serverURL, nil)

				if err != nil {
					log.Printf("连接失败: %v", err)
					log.Printf("将在 %v 后重试", reconnectInterval)
					time.Sleep(reconnectInterval)
					continue
				}

				log.Println("连接成功！")

				// 启动一个goroutine来监听消息
				go func() {
					defer func() {
						conn.Close()
						conn = nil
					}()

					// 设置读取超时
					conn.SetReadDeadline(time.Now().Add(60 * time.Second))
					conn.SetPingHandler(func(string) error {
						// 收到ping时重置超时
						conn.SetReadDeadline(time.Now().Add(60 * time.Second))
						return nil
					})

					for {
						msgType, message, err := conn.ReadMessage()
						if err != nil {
							log.Printf("读取错误: %v", err)
							return
						}
						if msgType == websocket.TextMessage {
							handleTextMessage(message)
						}
						// 重置读取超时
						conn.SetReadDeadline(time.Now().Add(60 * time.Second))
					}
				}()

				// 定期发送ping以保持连接活跃
				ticker := time.NewTicker(30 * time.Second)
				go func() {
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
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
				}()
			}

			// 等待重新连接检查
			time.Sleep(time.Second)
			if conn == nil {
				log.Printf("连接已断开，将在 %v 后重试", reconnectInterval)
				time.Sleep(reconnectInterval - time.Second) // 减去已经睡眠的1秒
			}
		}
	}
}
