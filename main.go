package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

// Config 应用配置结构体
type Config struct {
	NodeUID         string // 节点唯一标识
	RelayServerHost string // 中继服务器主机地址
}

// LoadConfig 从环境变量和命令行参数加载配置
func LoadConfig() (*Config, error) {
	// 加载.env文件
	err := godotenv.Load()
	if err != nil {
		log.Println("警告: 加载.env文件失败:", err)
		// 继续执行，因为用户可能通过命令行参数提供配置
	}

	// 初始化配置，从环境变量中加载
	config := &Config{
		NodeUID:         os.Getenv("NODE_UID"),
		RelayServerHost: os.Getenv("RELAY_SERVER_HOST"),
	}

	// 设置命令行参数
	cmdNodeUID := flag.String("node-uid", "", "指定 Node UID")
	cmdRelayServerHost := flag.String("relay-server-host", "", "指定 Relay Server Host")
	flag.Parse()

	// 命令行参数优先级高于环境变量
	if *cmdNodeUID != "" {
		config.NodeUID = *cmdNodeUID
		log.Printf("使用命令行参数指定的 Node UID: %s", config.NodeUID)
	} else if config.NodeUID != "" {
		log.Printf("使用环境变量中的 Node UID: %s", config.NodeUID)
	} else {
		return nil, fmt.Errorf("未指定 Node UID，请通过命令行参数 --node-uid 或环境变量 NODE_UID 设置")
	}

	if *cmdRelayServerHost != "" {
		config.RelayServerHost = *cmdRelayServerHost
		log.Printf("使用命令行参数指定的 Relay Server Host: %s", config.RelayServerHost)
	} else if config.RelayServerHost != "" {
		log.Printf("使用环境变量中的 Relay Server Host: %s", config.RelayServerHost)
	} else {
		return nil, fmt.Errorf("未指定 Relay Server Host，请通过命令行参数 --relay-server-host 或环境变量 RELAY_SERVER_HOST 设置")
	}

	return config, nil
}

type MessageType string

// DownloadInfo 下载信息结构体
type DownloadInfo struct {
	FileID      string // 文件唯一标识
	FileName    string // 文件名
	FileSize    int64  // 文件大小
	ChunkSize   int64  // 分片大小
	TotalChunks int    // 分片总数
	FileHash    string // 文件哈希值
	DownloadUrl string // 下载URL
}

const (
	Pong            MessageType = "pong"
	InitEnv         MessageType = "init_env"
	UpdateProject   MessageType = "update_project"   // 全量更新项目
	UpdateResources MessageType = "update_resources" // 增量更新资源
	ErrorReport     MessageType = "error_report"     // 错误上报
)

// 全局变量
var (
	wsConn    *websocket.Conn
	wsConnMu  sync.Mutex // 保护wsConn的互斥锁
	serverURL string     // 存储服务器URL
	config    *Config    // 全局配置对象
)

func init() {
	// 已由LoadConfig函数替代
}

func main() {
	// 设置日志
	log.SetOutput(os.Stdout)
	log.SetPrefix("[WebSocket Client] ")

	// 加载配置
	var err error
	config, err = LoadConfig()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 创建连接的URL
	u := url.URL{Scheme: "ws", Host: config.RelayServerHost, Path: "/socket/node/" + config.NodeUID}
	serverURL = u.String()
	log.Printf("连接到 %s", serverURL)

	// 创建一个上下文，用于处理程序终止
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 处理Ctrl+C信号
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// 启动一个goroutine来处理重连逻辑
	go maintainConnection(ctx, serverURL)

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

// ErrorData 定义错误上报的数据结构
type ErrorData struct {
	Source    string `json:"source"`    // 错误来源
	Message   string `json:"message"`   // 错误消息
	Timestamp int64  `json:"timestamp"` // 错误发生时间戳
	TaskID    string `json:"task_id"`   // 任务ID，如果有的话
}

// reportError 向服务器上报错误
func reportError(source string, errMsg string, taskID string) {
	wsConnMu.Lock()
	defer wsConnMu.Unlock()

	if wsConn == nil {
		log.Printf("无法上报错误，WebSocket连接未建立")
		return
	}

	errorData := ErrorData{
		Source:    source,
		Message:   errMsg,
		Timestamp: time.Now().Unix(),
		TaskID:    taskID,
	}

	data, err := json.Marshal(errorData)
	if err != nil {
		log.Printf("错误数据序列化失败: %v", err)
		return
	}

	message := map[string]interface{}{
		"type": ErrorReport,
		"data": json.RawMessage(data),
	}

	messageBytes, err := json.Marshal(message)
	if err != nil {
		log.Printf("错误消息序列化失败: %v", err)
		return
	}

	err = wsConn.WriteMessage(websocket.TextMessage, messageBytes)
	if err != nil {
		log.Printf("发送错误上报消息失败: %v", err)
		return
	}

	log.Printf("已上报错误: %s - %s", source, errMsg)
}

// 提取任务ID的辅助函数
func extractTaskID(data *any) string {
	if data == nil {
		return ""
	}

	// 尝试将data转换为map并提取taskID
	if m, ok := (*data).(map[string]interface{}); ok {
		if taskID, exists := m["task_id"]; exists {
			if taskIDStr, ok := taskID.(string); ok {
				return taskIDStr
			}
		}
	}

	return ""
}

func handleTextMessage(message []byte) {
	var textMessage TextMessage
	err := json.Unmarshal(message, &textMessage)
	if err != nil {
		log.Printf("解析消息失败: %v", err)
		reportError("message_parsing", fmt.Sprintf("解析消息失败: %v", err), "")
		return
	}

	// 提取任务ID，用于错误上报
	taskID := extractTaskID(textMessage.Data)

	switch textMessage.Type {
	case InitEnv:
		if err := handleInitEnv(); err != nil {
			log.Printf("处理init_env消息失败: %v", err)
			reportError(string(InitEnv), err.Error(), taskID)
		}
	case UpdateProject:
		if err := handleUpdateProject(textMessage.Data); err != nil {
			log.Printf("处理update_project消息失败: %v", err)
			reportError(string(UpdateProject), err.Error(), taskID)
		}
	case UpdateResources:
		if err := handleUpdateResource(textMessage.Data); err != nil {
			log.Printf("处理update_resources消息失败: %v", err)
			reportError(string(UpdateResources), err.Error(), taskID)
		}
	}
}

func handleInitEnv() error {
	// 检查 /opt/docker-apps 是否存在
	if _, err := os.Stat("/opt/docker-apps"); os.IsNotExist(err) {
		// 创建 /opt/docker-apps 目录
		err := os.MkdirAll("/opt/docker-apps", 0755)
		if err != nil {
			return fmt.Errorf("创建 /opt/docker-apps 目录失败: %v", err)
		}
	}

	// 检查 /opt/docker-apps 是否为空，不为空，则清空
	if _, err := os.Stat("/opt/docker-apps"); err == nil {
		err := os.RemoveAll("/opt/docker-apps")
		if err != nil {
			return fmt.Errorf("清空 /opt/docker-apps 目录失败: %v", err)
		}
	}

	// 压缩文件
	if err := downloadFile("stub.tar", "/opt/docker-apps"); err != nil {
		return fmt.Errorf("下载压缩文件失败: %v", err)
	}

	// 下载初始化脚本
	if err := downloadFile("setup", "/opt/docker-apps"); err != nil {
		return fmt.Errorf("下载初始化脚本失败: %v", err)
	}

	// 调用 setup 脚本，执行初始化
	setupScript := "/opt/docker-apps/setup"
	cmd := exec.Command(setupScript)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("执行初始化脚本失败: %v", err)
	}

	// 1. 检查解压软件是否安装
	// 2. 下载压缩文件
	// 3. 解压文件
	// 4. 执行初始化脚本
	// 5. 下载 compose 文件
	// 6. 启动 docker compose
	// 7. 上报启动结果

	return nil // 如果没有错误发生，返回 nil
}

// 下载文件到指定目录，使用分片下载
func downloadFile(filename string, destDir string) error {
	// 确定分片大小和数量
	chunkSize := int64(3 * 1024 * 1024) // 默认2MB分片大小
	parallelDownloads := 10             // 默认并行下载数

	// 网络状况检测 - 如果有需要可以调整分片大小
	// if netSpeed := detectNetworkSpeed(); netSpeed > 0 {
	// 	// 根据网络速度调整分片大小，速度越快分片越大
	// 	if netSpeed > 5*1024*1024 { // 5MB/s
	// 		chunkSize = 5 * 1024 * 1024
	// 		parallelDownloads = 5
	// 	} else if netSpeed < 512*1024 { // 512KB/s
	// 		chunkSize = 512 * 1024
	// 		parallelDownloads = 2
	// 	}
	// 	log.Printf("网络速度约为 %.2f MB/s, 调整分片大小为 %d, 并行数为 %d",
	// 		float64(netSpeed)/(1024*1024), chunkSize, parallelDownloads)
	// }

	// 使用config.RelayServerHost构建URL
	downloadUrl, err := url.Parse(fmt.Sprintf("http://%s/file/download/init", config.RelayServerHost))
	if err != nil {
		return fmt.Errorf("解析下载URL失败: %v", err)
	}

	queryParams := url.Values{}
	queryParams.Add("fileName", filename)
	queryParams.Add("chunkSize", strconv.Itoa(int(chunkSize)))
	downloadUrl.RawQuery = queryParams.Encode()

	resp, err := http.Get(downloadUrl.String())
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %v", err)
	}
	defer resp.Body.Close()

	// 反序列化为 DownloadInfo 结构体
	var downloadInfo DownloadInfo
	err = json.NewDecoder(resp.Body).Decode(&downloadInfo)
	if err != nil {
		return fmt.Errorf("反序列化失败: %v", err)
	}

	log.Printf("下载信息: %+v", downloadInfo)

	// 创建目标文件
	fileSize := downloadInfo.FileSize

	// 确保下载URL是绝对路径
	baseURL := fmt.Sprintf("http://%s", config.RelayServerHost)
	if !strings.HasPrefix(downloadInfo.DownloadUrl, "http") {
		downloadInfo.DownloadUrl = baseURL + downloadInfo.DownloadUrl
	}

	// 创建临时目录存放分片
	tempDir := os.TempDir()
	chunkDir := filepath.Join(tempDir, fmt.Sprintf("chunks_%s", downloadInfo.FileID))
	err = os.MkdirAll(chunkDir, 0755)
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(chunkDir) // 下载完成后清理临时目录

	// 创建目标目录（如果不存在）
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		err = os.MkdirAll(destDir, 0755)
		if err != nil {
			return fmt.Errorf("创建目标目录失败: %v", err)
		}
	}

	// 创建最终文件
	destFilePath := filepath.Join(destDir, downloadInfo.FileName)
	destFile, err := os.Create(destFilePath)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %v", err)
	}
	defer destFile.Close()
	// 预分配文件大小，避免磁盘碎片
	err = destFile.Truncate(fileSize)
	if err != nil {
		return fmt.Errorf("预分配文件大小失败: %v", err)
	}

	// 开始分片下载
	var wg sync.WaitGroup
	errorChan := make(chan error, downloadInfo.TotalChunks)
	semaphore := make(chan struct{}, parallelDownloads) // 限制并发数

	// 下载进度跟踪
	downloadedChunks := 0
	var downloadedMutex sync.Mutex
	startTime := time.Now()

	log.Printf("开始下载文件 %s，共 %d 个分片", downloadInfo.FileName, downloadInfo.TotalChunks)

	for i := 0; i < downloadInfo.TotalChunks; i++ {
		wg.Add(1)
		semaphore <- struct{}{} // 获取一个并发槽

		go func(chunkIndex int) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放并发槽

			chunkURL, err := url.Parse(fmt.Sprintf("http://%s/file/download/chunk", config.RelayServerHost))
			if err != nil {
				errorChan <- fmt.Errorf("解析服务器URL失败: %v", err)
				return
			}

			params := url.Values{}
			params.Add("fileID", downloadInfo.FileID)
			params.Add("chunkIndex", strconv.Itoa(chunkIndex))

			chunkURL.RawQuery = params.Encode()

			// 下载分片
			chunkResp, err := http.Get(chunkURL.String())
			if err != nil {
				errorChan <- fmt.Errorf("下载分片 %d 失败: %v", chunkIndex, err)
				return
			}
			defer chunkResp.Body.Close()

			// 对于分块下载，状态码200和206(Partial Content)都是有效的
			if chunkResp.StatusCode != http.StatusOK && chunkResp.StatusCode != http.StatusPartialContent {
				errorChan <- fmt.Errorf("下载分片 %d 失败，状态码: %d", chunkIndex, chunkResp.StatusCode)
				return
			}

			// 将分片保存到临时文件
			chunkFilePath := filepath.Join(chunkDir, fmt.Sprintf("chunk_%d", chunkIndex))
			chunkFile, err := os.Create(chunkFilePath)
			if err != nil {
				errorChan <- fmt.Errorf("创建分片文件 %d 失败: %v", chunkIndex, err)
				return
			}
			defer chunkFile.Close()

			_, err = io.Copy(chunkFile, chunkResp.Body)
			if err != nil {
				errorChan <- fmt.Errorf("保存分片 %d 内容失败: %v", chunkIndex, err)
				return
			}

			// 更新下载进度
			downloadedMutex.Lock()
			downloadedChunks++
			progress := float64(downloadedChunks) / float64(downloadInfo.TotalChunks) * 100
			elapsedTime := time.Since(startTime).Seconds()
			speed := float64(downloadedChunks) * float64(chunkSize) / elapsedTime / 1024 / 1024
			log.Printf("下载进度: %.2f%% (%d/%d), 速度: %.2f MB/s",
				progress, downloadedChunks, downloadInfo.TotalChunks, speed)
			downloadedMutex.Unlock()
		}(i)
	}

	// 等待所有下载完成
	wg.Wait()
	close(errorChan)

	// 检查是否有错误
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		errMsgs := ""
		for _, err := range errors {
			errMsgs += err.Error() + "\n"
		}
		return fmt.Errorf("分片下载过程中发生错误:\n%s", errMsgs)
	}

	log.Printf("所有分片下载完成，开始合并文件...")

	// 下载完毕，合并文件
	destFile, err = os.OpenFile(destFilePath, os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("打开目标文件失败: %v", err)
	}
	defer destFile.Close()

	// 按顺序合并分片
	chunkSizeInt := downloadInfo.ChunkSize

	for i := 0; i < downloadInfo.TotalChunks; i++ {
		chunkFilePath := filepath.Join(chunkDir, fmt.Sprintf("chunk_%d", i))
		chunkFile, err := os.Open(chunkFilePath)
		if err != nil {
			return fmt.Errorf("打开分片文件 %d 失败: %v", i, err)
		}

		// 计算分片在文件中的偏移位置
		offset := int64(i) * chunkSizeInt

		// 将分片内容写入目标文件的正确位置
		_, err = destFile.Seek(offset, io.SeekStart)
		if err != nil {
			chunkFile.Close()
			return fmt.Errorf("定位目标文件位置失败: %v", err)
		}

		_, err = io.Copy(destFile, chunkFile)
		chunkFile.Close()
		if err != nil {
			return fmt.Errorf("合并分片 %d 失败: %v", i, err)
		}
	}

	log.Printf("文件合并完成，开始验证文件完整性...")

	// 验证文件完整性
	err = verifyFileIntegrity(destFilePath, downloadInfo.FileHash)
	if err != nil {
		return fmt.Errorf("文件完整性校验失败: %v", err)
	}

	log.Printf("文件 %s 下载并验证成功", downloadInfo.FileName)
	return nil
}

// 文件信息结构
type FileInfo struct {
	Name string
	Size int64
	Hash string // 可选的文件哈希，用于完整性校验
}

// 验证文件完整性
func verifyFileIntegrity(filePath, expectedHash string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 分块计算哈希值
	hasher := sha256.New()
	bufSize := 4 * 1024 * 1024 // 4MB缓冲区
	buf := make([]byte, bufSize)

	for {
		n, err := file.Read(buf)
		if n > 0 {
			hasher.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取文件失败: %v", err)
		}
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("哈希值不匹配，预期: %s, 实际: %s", expectedHash, actualHash)
	}

	return nil
}

// 检测网络速度（简化版）
func detectNetworkSpeed() int64 {
	// 这里是一个简化的网络速度检测
	// 在实际应用中，可能需要更复杂的逻辑来获取真实网速

	// 下载一个小文件来测试速度
	startTime := time.Now()
	resp, err := http.Get(fmt.Sprintf("http://%s/v1/file/speedtest", config.RelayServerHost))
	if err != nil {
		log.Printf("网络速度检测失败: %v", err)
		return 0
	}
	defer resp.Body.Close()

	// 读取响应内容
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("读取速度测试响应失败: %v", err)
		return 0
	}

	// 计算速度（字节/秒）
	duration := time.Since(startTime).Seconds()
	if duration > 0 {
		speed := int64(float64(len(data)) / duration)
		return speed
	}

	return 0
}

func handleUpdateProject(data *any) error {
	log.Printf("收到update_project消息: %v", data)
	// 1. 下载项目
	// 2. 解压项目
	// 3. 执行初始化脚本
	// 4. 上报启动结果

	return nil // 如果没有错误发生，返回nil
}

func handleUpdateResource(data *any) error {
	log.Printf("收到update_resource消息: %v", data)
	// 1. 下载资源
	// 2. 解压资源
	// 3. 执行初始化脚本
	// 4. 上报启动结果

	return nil // 如果没有错误发生，返回nil
}

func maintainConnection(ctx context.Context, serverURL string) {
	const reconnectInterval = 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			// 上下文已取消，退出函数
			wsConnMu.Lock()
			if wsConn != nil {
				wsConn.Close()
				wsConn = nil
			}
			wsConnMu.Unlock()
			return
		default:
			// 尝试连接
			wsConnMu.Lock()
			isConnNil := wsConn == nil
			wsConnMu.Unlock()

			if isConnNil {
				log.Printf("正在连接到 %s...", serverURL)

				conn, _, dialErr := websocket.DefaultDialer.Dial(serverURL, nil)
				if dialErr != nil {
					log.Printf("连接失败: %v", dialErr)
					log.Printf("将在 %v 后重试", reconnectInterval)
					time.Sleep(reconnectInterval)
					continue
				}

				wsConnMu.Lock()
				wsConn = conn
				wsConnMu.Unlock()

				log.Println("连接成功！")

				// 启动一个goroutine来监听消息
				go func() {
					defer func() {
						wsConnMu.Lock()
						if wsConn != nil {
							wsConn.Close()
							wsConn = nil
						}
						wsConnMu.Unlock()
					}()

					// 设置读取超时
					wsConnMu.Lock()
					if wsConn != nil {
						wsConn.SetReadDeadline(time.Now().Add(60 * time.Second))
						wsConn.SetPingHandler(func(string) error {
							// 收到ping时重置超时
							wsConn.SetReadDeadline(time.Now().Add(60 * time.Second))
							return nil
						})
					}
					wsConnMu.Unlock()

					for {
						wsConnMu.Lock()
						conn := wsConn
						wsConnMu.Unlock()

						if conn == nil {
							return
						}

						msgType, message, err := conn.ReadMessage()
						if err != nil {
							log.Printf("读取错误: %v", err)
							return
						}
						if msgType == websocket.TextMessage {
							handleTextMessage(message)
						}
						// 重置读取超时
						wsConnMu.Lock()
						if wsConn != nil {
							wsConn.SetReadDeadline(time.Now().Add(60 * time.Second))
						}
						wsConnMu.Unlock()
					}
				}()

				// 定期发送ping以保持连接活跃
				ticker := time.NewTicker(3 * time.Second)
				go func() {
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							wsConnMu.Lock()
							conn := wsConn
							wsConnMu.Unlock()

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
				}()
			}

			// 等待重新连接检查
			time.Sleep(time.Second)

			wsConnMu.Lock()
			isConnNil = wsConn == nil
			wsConnMu.Unlock()

			if isConnNil {
				log.Printf("连接已断开，将在 %v 后重试", reconnectInterval)
				time.Sleep(reconnectInterval - time.Second) // 减去已经睡眠的1秒
			}
		}
	}
}
