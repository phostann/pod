package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"

	"com.example/pod/config"
	"com.example/pod/downloader"
	"com.example/pod/model"
	"com.example/pod/reporter"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MessageHandler 处理WebSocket消息
type MessageHandler struct {
	downloader    *downloader.FileDownloader
	errorReporter *reporter.ErrorReporter
	config        *config.Config
}

// NewMessageHandler 创建消息处理器
func NewMessageHandler(downloader *downloader.FileDownloader, errorReporter *reporter.ErrorReporter, config *config.Config) *MessageHandler {
	return &MessageHandler{
		downloader:    downloader,
		errorReporter: errorReporter,
		config:        config,
	}
}

// parseMessage 解析消息并提取taskID
func (h *MessageHandler) parseMessage(message []byte) (model.TextMessage, error) {
	var textMessage model.TextMessage
	if err := json.Unmarshal(message, &textMessage); err != nil {
		log.Printf("解析消息失败: %v", err)
		h.errorReporter.ReportError("message_parsing", fmt.Sprintf("解析消息失败: %v", err), "")
		return textMessage, err
	}

	// 提取任务ID用于错误上报
	return textMessage, nil
}

// reportError 报告错误并返回包装后的错误
func (h *MessageHandler) reportError(msgType model.MessageType, format string, err error) error {
	errMsg := fmt.Errorf(format, err)
	h.errorReporter.ReportError(string(msgType), errMsg.Error(), "")
	return errMsg
}

// HandleInitNode 处理初始化节点消息
func (h *MessageHandler) HandleInitNode(message []byte) error {
	// 解析消息获取可能的任务ID，用于错误报告
	_, err := h.parseMessage(message)
	if err != nil {
		return err
	}

	// 检查 /opt/docker-apps 是否存在
	log.Printf("检查 /opt/docker-apps 是否存在")
	if _, err := os.Stat("/opt/docker-apps"); os.IsNotExist(err) {
		// 创建 /opt/docker-apps 目录
		err := os.MkdirAll("/opt/docker-apps", 0755)
		if err != nil {
			return h.reportError(model.InitNode, "创建 /opt/docker-apps 目录失败: %v", err)
		}
	}

	// 检查 /opt/docker-apps 是否为空，不为空，则清空
	if _, err := os.Stat("/opt/docker-apps"); err == nil {
		err := os.RemoveAll("/opt/docker-apps")
		if err != nil {
			return h.reportError(model.InitNode, "清空 /opt/docker-apps 目录失败: %v", err)
		}
	}

	log.Printf("下载压缩文件")
	// 下载压缩文件
	if err := h.downloader.DownloadFileInChunks("stub.tar", "/opt/docker-apps"); err != nil {
		return h.reportError(model.InitNode, "下载压缩文件失败: %v", err)
	}

	log.Printf("下载 compose.yaml 文件")
	if err := h.downloader.DownloadFileInChunks(fmt.Sprintf("%s.yaml", h.config.NodeUID), "/opt/docker-apps", "compose.yaml"); err != nil {
		return h.reportError(model.InitNode, "下载 compose.yaml 文件失败: %v", err)
	}

	log.Printf("下载初始化脚本")
	// 下载初始化脚本
	if err := h.downloader.DownloadFileInChunks("setup", "/opt/docker-apps"); err != nil {
		return h.reportError(model.InitNode, "下载初始化脚本失败: %v", err)
	}

	// 赋予可执行权限
	err = os.Chmod("/opt/docker-apps/setup", 0755)
	if err != nil {
		return h.reportError(model.InitNode, "赋予可执行权限失败: %v", err)
	}

	// 调用 setup 脚本，执行初始化
	setupScript := "/opt/docker-apps/setup"
	cmd := exec.Command(setupScript)
	cmd.Dir = "/opt/docker-apps"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		return h.reportError(model.InitNode, "执行初始化脚本失败: %v", err)
	}

	return nil // 如果没有错误发生，返回 nil
}

// HandleUpdateProject 处理更新项目消息
func (h *MessageHandler) HandleUpdateProject(message []byte) error {
	textMessage, err := h.parseMessage(message)
	if err != nil {
		return err
	}

	// 处理消息
	log.Printf("收到update_project消息: %v", textMessage.Data)
	// 1. 下载项目
	// 2. 解压项目
	// 3. 执行初始化脚本
	// 4. 上报启动结果

	// 如果出现错误，确保包含任务ID
	if false { // 示例条件，实际根据处理结果判断
		return h.reportError(model.UpdateProject, "示例错误", fmt.Errorf("示例错误"))
	}

	return nil // 如果没有错误发生，返回nil
}

// HandleUpdateResource 处理更新资源消息
func (h *MessageHandler) HandleUpdateResource(message []byte) error {
	textMessage, err := h.parseMessage(message)
	if err != nil {
		return err
	}

	// 处理消息
	log.Printf("收到update_resource消息: %v", textMessage.Data)
	// 1. 下载资源
	// 2. 解压资源
	// 3. 执行初始化脚本
	// 4. 上报启动结果

	// 如果出现错误，确保包含任务ID
	if false { // 示例条件，实际根据处理结果判断
		return h.reportError(model.UpdateResources, "示例错误", fmt.Errorf("示例错误"))
	}

	return nil // 如果没有错误发生，返回nil
}

// HandleSync 处理同步资源消息
func (h *MessageHandler) HandleSync(message []byte) error {
	textMessage, err := h.parseMessage(message)
	if err != nil {
		return err
	}

	log.Printf("收到sync消息: %v", textMessage.Data)

	// 从Data中提取uid和filename
	messageData, ok := textMessage.Data.(map[string]interface{})
	if !ok {
		return h.reportError(model.Sync, "无效的消息格式", fmt.Errorf("无法解析消息数据"))
	}

	// 从messageData中获取uid和filename，使用蛇形命名法作为键名
	uid, ok := messageData["uid"].(string)
	if !ok {
		return h.reportError(model.Sync, "无效的uid", fmt.Errorf("uid不存在或无效"))
	}

	filename, ok := messageData["filename"].(string)
	if !ok {
		return h.reportError(model.Sync, "无效的filename", fmt.Errorf("filename不存在或无效"))
	}

	downloadUrl, err := url.Parse(fmt.Sprintf("http://%s/sync/download", h.config.RelayServerHost))
	if err != nil {
		return h.reportError(model.Sync, "解析下载URL失败: %v", err)
	}

	params := url.Values{}
	params.Add("filename", fmt.Sprintf("%s/%s", uid, filename))
	downloadUrl.RawQuery = params.Encode()

	resp, err := http.Get(downloadUrl.String())
	if err != nil {
		return h.reportError(model.Sync, "下载文件失败: %v", err)
	}
	defer resp.Body.Close()

	minioClient, err := h.getMinIoClient()
	if err != nil {
		return h.reportError(model.Sync, "创建Minio客户端失败: %v", err)
	}

	// 上传文件至 minio

	exists, err := minioClient.BucketExists(context.Background(), h.config.MinioBucket)
	if err != nil {
		return h.reportError(model.Sync, "检查Bucket是否存在失败: %v", err)
	}

	if !exists {
		// 创建Bucket
		err = minioClient.MakeBucket(context.Background(), h.config.MinioBucket, minio.MakeBucketOptions{})
		if err != nil {
			return h.reportError(model.Sync, "创建Bucket失败: %v", err)
		}
	}

	// 上传文件
	_, err = minioClient.PutObject(context.Background(), h.config.MinioBucket, filename, resp.Body, -1, minio.PutObjectOptions{})
	if err != nil {
		return h.reportError(model.Sync, "上传文件失败: %v", err)
	}

	// 调用 /sync/complete 接口
	completeUrl, err := url.Parse(fmt.Sprintf("http://%s/sync/complete", h.config.RelayServerHost))
	if err != nil {
		return h.reportError(model.Sync, "解析完成URL失败: %v", err)
	}

	params = url.Values{}
	params.Add("uid", uid)
	params.Add("filename", filename)
	completeUrl.RawQuery = params.Encode()

	resp, err = http.Post(completeUrl.String(), "application/json", nil)
	if err != nil {
		return h.reportError(model.Sync, "调用 /sync/complete 接口失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return h.reportError(model.Sync, "调用 /sync/complete 接口失败: %v", fmt.Errorf("状态码: %d", resp.StatusCode))
	}

	return nil // 如果没有错误发生，返回nil
}

func (h *MessageHandler) getMinIoClient() (*minio.Client, error) {

	minioClient, err := minio.New(
		h.config.MinioEndpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(h.config.MinioAccessKey, h.config.MinioSecretKey, ""),
			Secure: false,
		},
	)
	if err != nil {
		return nil, h.reportError(model.Sync, "创建Minio客户端失败: %v", err)
	}

	return minioClient, nil
}
