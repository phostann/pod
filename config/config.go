package config

import (
	"flag"
	"fmt"
	"log"
	"os"

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
