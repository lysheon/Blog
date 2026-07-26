// Package database 提供 MySQL 连接与连接池配置。
//
// 使用 GORM v2 作为数据访问层；生产 Schema 不依赖 AutoMigrate，
// 而由 cmd/migrate 执行显式 SQL Migration。
package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/lsy/blog/internal/config"
)

// newQueryLogger builds the GORM logger. Parameterized queries keep statements
// readable for debugging while keeping row values — emails, password hashes,
// tokens, article bodies — out of the application log, which is shipped to
// operators and log storage.
func newQueryLogger(env string, writer io.Writer) gormlogger.Interface {
	logLevel := gormlogger.Warn
	if env == "dev" {
		logLevel = gormlogger.Info
	}
	return gormlogger.New(log.New(writer, "", log.LstdFlags), gormlogger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logLevel,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		Colorful:                  false,
	})
}

// New 根据配置创建 GORM DB 连接。
func New(ctx context.Context, cfg config.MySQLConfig, env string) (*gorm.DB, error) {
	if cfg.DSN == "" {
		return nil, errors.New("database: MYSQL_DSN is required")
	}

	gormCfg := &gorm.Config{
		Logger: newQueryLogger(env, os.Stdout),
		// 使用下方带 context 超时的单次 Ping，避免 GORM 在连接池配置前无界 Ping。
		DisableAutomaticPing: true,
		// 关闭事务嵌套回滚点的隐式行为，保持事务语义可预测。
		SkipDefaultTransaction: false,
	}

	db, err := gorm.Open(mysql.Open(cfg.DSN), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("database: open mysql: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("database: get *sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	// 启动时验证连通性，避免在请求期才发现 DSN 错误。
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(pingCtx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("database: ping mysql: %w", err)
	}

	return db, nil
}

// Close 关闭底层连接池。
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
