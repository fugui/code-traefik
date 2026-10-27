package config

import (
	"log"
	"os"
	"strings"

	"code-common/backend/configutil"
	commonModels "code-common/backend/models"

	"gopkg.in/yaml.v3"
)

// 模块级核心元数据常量（集中声明，便于 scripts/init-module.sh 或手工派生时一站式替换）
const (
	ModuleKey   = "traefik"
	ServiceName = "Code-Traefik"
	AdminRole   = "traefik_admin"
	TablePrefix = "traefik_"
	DefaultPort = ":8091"
)

// AuthSection 定义模块认证与独立运行开关配置
type AuthSection struct {
	JWTSecret            string                    `yaml:"jwt_secret"`
	StandaloneMode       bool                      `yaml:"standalone_mode"`
	PasswordLoginEnabled bool                      `yaml:"password_login_enabled"`
	OAuth2               commonModels.OAuth2Config `yaml:"oauth2"`
}

// Config 定义模块全局配置结构
type Config struct {
	Server   configutil.ServerConfig     `yaml:"server"`
	Database commonModels.DatabaseConfig `yaml:"database"`
	Auth     AuthSection                 `yaml:"auth"`
}

// AppConfig 全局运行时配置单例
var AppConfig Config

// LoadConfig 从指定路径加载 YAML 配置；若不存在则尝试回退至 config.yaml.example 或默认配置
func LoadConfig(filename string) error {
	AppConfig = Config{}

	actualPath := filename
	if _, err := os.Stat(actualPath); os.IsNotExist(err) {
		if _, exErr := os.Stat("config.yaml.example"); exErr == nil {
			log.Printf("[%s] 未检测到 %s，回退加载 config.yaml.example", ServiceName, filename)
			actualPath = "config.yaml.example"
		} else {
			log.Printf("[%s] 配置文件不存在，启用内置默认安全配置", ServiceName)
			applyDefaults()
			return nil
		}
	}

	data, err := os.ReadFile(actualPath)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(data, &AppConfig); err != nil {
		return err
	}

	applyDefaults()
	return nil
}

func applyDefaults() {
	configutil.ApplyServerDefaults(&AppConfig.Server, DefaultPort)

	// 支持环境变量覆盖 JWT_SECRET（生产环境推荐通过环境变量或密钥管理系统注入）
	// TODO(security): 生产环境集群部署时请接入企业 KMS / Secret Manager 注入统一密钥，禁用文件明文落盘
	if envSecret := strings.TrimSpace(os.Getenv("JWT_SECRET")); envSecret != "" {
		AppConfig.Auth.JWTSecret = envSecret
	}
	configutil.EnsureJWTSecret(&AppConfig.Auth.JWTSecret, ServiceName)

	if AppConfig.Auth.OAuth2.Enabled {
		if len(AppConfig.Auth.OAuth2.Scopes) == 0 {
			AppConfig.Auth.OAuth2.Scopes = []string{"openid", "profile", "email"}
		}
		if AppConfig.Auth.OAuth2.FieldMapping.Username == "" {
			AppConfig.Auth.OAuth2.FieldMapping.Username = "preferred_username"
		}
		if AppConfig.Auth.OAuth2.FieldMapping.Email == "" {
			AppConfig.Auth.OAuth2.FieldMapping.Email = "email"
		}
		if AppConfig.Auth.OAuth2.FieldMapping.Name == "" {
			AppConfig.Auth.OAuth2.FieldMapping.Name = "name"
		}
		if AppConfig.Auth.OAuth2.RedirectURL == "" {
			AppConfig.Auth.OAuth2.RedirectURL = strings.TrimRight(AppConfig.Server.ExternalURL, "/") + "/api/oauth2/callback"
		}
	}
}
