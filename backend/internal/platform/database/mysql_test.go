package database

import (
	"bytes"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// loggedRow mirrors the sensitive columns the application actually writes,
// without pulling in domain associations that would need a live server.
type loggedRow struct {
	ID           uint64 `gorm:"primaryKey"`
	Email        string
	PasswordHash string
}

func (loggedRow) TableName() string { return "users" }

const (
	sensitiveEmail = "leak-check@example.test"
	sensitiveHash  = "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA"
)

// dev is the noisiest log level, so it is the level that must prove row values
// never reach the log. Writes carry them in bind variables and reads carry them
// in the WHERE clause, so both directions are covered.
func TestQueryLoggerKeepsRowValuesOutOfLogs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		statement func(*gorm.DB)
		wantSQL   string
	}{
		{
			name:      "insert",
			statement: func(db *gorm.DB) { db.Create(&loggedRow{Email: sensitiveEmail, PasswordHash: sensitiveHash}) },
			wantSQL:   "INSERT INTO",
		},
		{
			name: "select",
			statement: func(db *gorm.DB) {
				db.Where("email = ? AND password_hash = ?", sensitiveEmail, sensitiveHash).Find(&[]loggedRow{})
			},
			wantSQL: "SELECT",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var logged bytes.Buffer
			test.statement(newDryRunDB(t, "dev", &logged))

			output := logged.String()
			if !strings.Contains(output, test.wantSQL) {
				t.Fatalf("statement was not logged at all, so the log lost its debugging value:\n%s", output)
			}
			for _, secret := range []string{sensitiveEmail, sensitiveHash} {
				if strings.Contains(output, secret) {
					t.Fatalf("log leaked %q:\n%s", secret, output)
				}
			}
			if !strings.Contains(output, "?") {
				t.Fatalf("log should keep bind placeholders instead of inlined values:\n%s", output)
			}
		})
	}
}

func TestQueryLoggerStaysQuietOutsideDev(t *testing.T) {
	t.Parallel()

	var logged bytes.Buffer
	db := newDryRunDB(t, "production", &logged)

	db.Create(&loggedRow{Email: sensitiveEmail, PasswordHash: sensitiveHash})

	if logged.Len() != 0 {
		t.Fatalf("successful statements must not be logged outside dev:\n%s", logged.String())
	}
}

// newDryRunDB builds SQL through the real callbacks and logger without
// contacting a server, so the logging contract stays covered by unit tests.
// The default transaction is skipped because opening one would need a live
// connection; it does not affect what the logger emits.
func newDryRunDB(t *testing.T, env string, logged *bytes.Buffer) *gorm.DB {
	t.Helper()
	dialector := mysql.New(mysql.Config{
		DSN:                       "blog:test-only@tcp(127.0.0.1:1)/blog?charset=utf8mb4&parseTime=true",
		SkipInitializeWithVersion: true,
	})
	db, err := gorm.Open(dialector, &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 newQueryLogger(env, logged),
	})
	if err != nil {
		t.Fatalf("gorm.Open(): %v", err)
	}
	return db
}
