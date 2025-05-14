package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"com.example/pod/config"
	"com.example/pod/downloader"
	"com.example/pod/handler"
	"com.example/pod/model"
	"com.example/pod/reporter"
	"com.example/pod/ws"
)

// 主函数
func main() {
	// 设置日志
	log.SetOutput(os.Stdout)
	log.SetPrefix("[WebSocket Client] ")

	// 加载配置
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 创建连接的URL
	serverURL := ws.GenerateServerURL(cfg.RelayServerHost, cfg.NodeUID)
	log.Printf("连接到 %s", serverURL)

	// 创建一个上下文，用于处理程序终止
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 创建WebSocket连接管理器
	wsManager := ws.NewManager(cfg)

	// 创建下载器
	fileDownloader := downloader.NewFileDownloader(cfg.RelayServerHost)

	// 创建错误报告工具
	errorReporter := reporter.NewErrorReporter(wsManager)

	// 创建消息处理器
	msgHandler := handler.NewMessageHandler(fileDownloader, errorReporter, cfg)

	// 直接注册具体的消息处理函数，更简洁优雅
	wsManager.RegisterHandler(string(model.InitNode), msgHandler.HandleInitNode)
	wsManager.RegisterHandler(string(model.UpdateProject), msgHandler.HandleUpdateProject)
	wsManager.RegisterHandler(string(model.UpdateResources), msgHandler.HandleUpdateResource)

	// 启动一个goroutine来处理重连逻辑
	go wsManager.MaintainConnection(ctx)

	// 处理Ctrl+C信号
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// 等待中断信号
	<-interrupt
	log.Println("接收到中断信号，正在关闭...")
	cancel()
	log.Println("程序已关闭")
}
