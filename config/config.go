package config

import (
	"fmt"
	"log"
	"os"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// Config 应用配置结构体
type Config struct {
	NodeUID         string // 节点唯一标识
	RelayServerHost string // 中继服务器主机地址
	MinioEndpoint   string // Minio 端点
	MinioAccessKey  string // Minio 访问密钥
	MinioSecretKey  string // Minio 密钥
	MinioBucket     string // Minio 桶
}

// LoadConfig 使用 Viper 从环境变量和命令行参数加载配置
func LoadConfig() (*Config, error) {
	// 配置命令行参数
	flags := pflag.NewFlagSet("config", pflag.ContinueOnError)
	flags.String("node-uid", "", "指定 Node UID")
	flags.String("relay-server-host", "", "指定 Relay Server Host")
	flags.String("minio-endpoint", "", "指定 Minio 端点")
	flags.String("minio-access-key", "", "指定 Minio 访问密钥")
	flags.String("minio-secret-key", "", "指定 Minio 密钥")
	flags.String("minio-bucket", "", "指定 Minio 桶")

	// 尝试解析命令行参数，忽略错误
	flags.Parse(os.Args[1:])

	v := viper.New()

	// 设置配置项默认值
	v.SetDefault("node_uid", "")
	v.SetDefault("relay_server_host", "")
	v.SetDefault("minio_endpoint", "")
	v.SetDefault("minio_access_key", "")
	v.SetDefault("minio_secret_key", "")
	v.SetDefault("minio_bucket", "")

	// 将命令行参数绑定到viper
	v.BindPFlag("node_uid", flags.Lookup("node-uid"))
	v.BindPFlag("relay_server_host", flags.Lookup("relay-server-host"))
	v.BindPFlag("minio_endpoint", flags.Lookup("minio-endpoint"))
	v.BindPFlag("minio_access_key", flags.Lookup("minio-access-key"))
	v.BindPFlag("minio_secret_key", flags.Lookup("minio-secret-key"))
	v.BindPFlag("minio_bucket", flags.Lookup("minio-bucket"))

	// 支持从环境变量读取配置
	v.AutomaticEnv()

	// 环境变量名映射（大写）
	v.SetEnvPrefix("") // 不设置前缀
	v.BindEnv("node_uid", "NODE_UID")
	v.BindEnv("relay_server_host", "RELAY_SERVER_HOST")
	v.BindEnv("minio_endpoint", "MINIO_ENDPOINT")
	v.BindEnv("minio_access_key", "MINIO_ACCESS_KEY")
	v.BindEnv("minio_secret_key", "MINIO_SECRET_KEY")
	v.BindEnv("minio_bucket", "MINIO_BUCKET")

	// 支持从配置文件读取（可选）
	v.SetConfigName("config")   // 配置文件名称（无扩展名）
	v.SetConfigType("yaml")     // 配置文件类型
	v.AddConfigPath(".")        // 在当前目录查找
	v.AddConfigPath("/etc/pod") // 在 /etc/pod 目录查找

	// 尝试读取配置文件（失败不报错）
	if err := v.ReadInConfig(); err == nil {
		log.Println("使用配置文件:", v.ConfigFileUsed())
	}

	// 创建配置对象
	config := &Config{
		NodeUID:         v.GetString("node_uid"),
		RelayServerHost: v.GetString("relay_server_host"),
		MinioEndpoint:   v.GetString("minio_endpoint"),
		MinioAccessKey:  v.GetString("minio_access_key"),
		MinioSecretKey:  v.GetString("minio_secret_key"),
		MinioBucket:     v.GetString("minio_bucket"),
	}

	// 检查必须的配置项
	if config.NodeUID == "" {
		return nil, fmt.Errorf("未指定 Node UID，请通过命令行参数 --node-uid 或环境变量 NODE_UID 设置")
	}

	if config.RelayServerHost == "" {
		return nil, fmt.Errorf("未指定 Relay Server Host，请通过命令行参数 --relay-server-host 或环境变量 RELAY_SERVER_HOST 设置")
	}

	if config.MinioEndpoint == "" {
		return nil, fmt.Errorf("未指定 Minio Endpoint，请通过命令行参数 --minio-endpoint 或环境变量 MINIO_ENDPOINT 设置")
	}

	if config.MinioAccessKey == "" {
		return nil, fmt.Errorf("未指定 Minio Access Key，请通过命令行参数 --minio-access-key 或环境变量 MINIO_ACCESS_KEY 设置")
	}

	if config.MinioSecretKey == "" {
		return nil, fmt.Errorf("未指定 Minio Secret Key，请通过命令行参数 --minio-secret-key 或环境变量 MINIO_SECRET_KEY 设置")
	}

	if config.MinioBucket == "" {
		return nil, fmt.Errorf("未指定 Minio Bucket，请通过命令行参数 --minio-bucket 或环境变量 MINIO_BUCKET 设置")
	}

	return config, nil
}
