package models

import (
	"time"

	"github.com/google/uuid"
)

type Source struct {
	ID           uuid.UUID
	Name         string
	DownloadURL  string
	FileType     string // "csv" or "excel"
	ScheduleTime time.Time
	Active       bool
	CreatedAt    time.Time
}

type FileStatus string

const (
	FileStatusDownloaded FileStatus = "downloaded"
	FileStatusProcessing FileStatus = "processing"
	FileStatusCompleted  FileStatus = "completed"
	FileStatusFailed     FileStatus = "failed"
)

type File struct {
	ID        uuid.UUID
	SourceID  uuid.UUID
	FileDate  time.Time
	LocalPath string
	Status    FileStatus
	Checksum  string
	CreatedAt time.Time
}

type ExchangeRate struct {
	Currency  string
	RateToUSD float64
	RateDate  time.Time
}

type Revenue struct {
	ID            uuid.UUID
	SourceID      uuid.UUID
	RevenueDate   time.Time
	OriginalValue float64
	Currency      string
	USDValue      float64
	RawPayload    []byte // JSONB stored as raw bytes
	CreatedAt     time.Time
}
