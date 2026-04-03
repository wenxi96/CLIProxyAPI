package usage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
)

const (
	StatisticsFileVersion    = 1
	StatisticsFileName       = "usage-statistics.snapshot"
	legacyStatisticsFileName = "usage-statistics.json"
)

// StatisticsFilePayload 是 usage 快照文件的磁盘格式。
// 它与管理接口导出结构保持兼容，便于复用现有导入链路。
type StatisticsFilePayload struct {
	Version    int                `json:"version"`
	ExportedAt time.Time          `json:"exported_at"`
	Usage      StatisticsSnapshot `json:"usage"`
}

// StatisticsFilePath 返回 usage 快照文件的默认保存路径。
func StatisticsFilePath(cfg *config.Config) string {
	logDir := strings.TrimSpace(logging.ResolveLogDirectory(cfg))
	if logDir == "" {
		return StatisticsFileName
	}
	return filepath.Join(filepath.Clean(logDir), StatisticsFileName)
}

func legacyStatisticsFilePath(path string) string {
	target := strings.TrimSpace(path)
	if target == "" {
		return ""
	}
	target = filepath.Clean(target)
	if !strings.EqualFold(filepath.Base(target), StatisticsFileName) {
		return ""
	}
	return filepath.Join(filepath.Dir(target), legacyStatisticsFileName)
}

// SaveSnapshotFile 以原子方式写入完整 usage 快照。
func SaveSnapshotFile(path string, snapshot StatisticsSnapshot) error {
	payload := StatisticsFilePayload{
		Version:    StatisticsFileVersion,
		ExportedAt: time.Now().UTC(),
		Usage:      snapshot,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("usage: marshal snapshot file: %w", err)
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data)
}

// LoadSnapshotFile 从磁盘读取 usage 快照。
// 兼容带 envelope 的新格式和旧的裸 StatisticsSnapshot 格式。
func LoadSnapshotFile(path string) (StatisticsSnapshot, error) {
	var snapshot StatisticsSnapshot

	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return snapshot, fmt.Errorf("usage: statistics file is empty")
	}

	var envelope map[string]json.RawMessage
	if errEnvelope := json.Unmarshal(trimmed, &envelope); errEnvelope == nil {
		if _, ok := envelope["usage"]; ok {
			var payload StatisticsFilePayload
			if errPayload := json.Unmarshal(trimmed, &payload); errPayload != nil {
				return snapshot, fmt.Errorf("usage: decode snapshot payload: %w", errPayload)
			}
			if payload.Version != 0 && payload.Version != StatisticsFileVersion {
				return snapshot, fmt.Errorf("usage: unsupported snapshot version %d", payload.Version)
			}
			return payload.Usage, nil
		}
	}

	if errSnapshot := json.Unmarshal(trimmed, &snapshot); errSnapshot != nil {
		return snapshot, fmt.Errorf("usage: decode snapshot: %w", errSnapshot)
	}
	return snapshot, nil
}

// RestoreRequestStatistics 将磁盘快照合并到当前内存统计中。
// 快照文件不存在时返回 no-op。
func RestoreRequestStatistics(path string, stats *RequestStatistics) (loaded bool, result MergeResult, err error) {
	if stats == nil {
		return false, result, nil
	}
	_, versionBefore, persistedBefore := stats.SnapshotWithState()
	snapshot, errLoad := LoadSnapshotFile(path)
	if errLoad != nil {
		if os.IsNotExist(errLoad) {
			if legacyPath := legacyStatisticsFilePath(path); legacyPath != "" {
				snapshot, errLoad = LoadSnapshotFile(legacyPath)
			}
			if os.IsNotExist(errLoad) {
				return false, result, nil
			}
		}
		if errLoad != nil {
			return false, result, errLoad
		}
	}
	result = stats.MergeSnapshot(snapshot)
	if versionBefore == persistedBefore {
		stats.MarkAllPersisted()
	}
	return true, result, nil
}

// PersistRequestStatistics 将当前 usage 快照保存到磁盘。
// 只有存在未持久化变更时才会执行写盘。
func PersistRequestStatistics(path string, stats *RequestStatistics) (bool, error) {
	if stats == nil {
		return false, nil
	}
	snapshot, version, persistedVersion := stats.SnapshotWithState()
	if version == persistedVersion {
		return false, nil
	}
	if err := SaveSnapshotFile(path, snapshot); err != nil {
		return false, err
	}
	stats.MarkPersisted(version)
	return true, nil
}

func writeFileAtomic(path string, data []byte) error {
	target := strings.TrimSpace(path)
	if target == "" {
		return fmt.Errorf("usage: empty snapshot path")
	}
	target = filepath.Clean(target)

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("usage: create snapshot directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, "usage-statistics-*.tmp")
	if err != nil {
		return fmt.Errorf("usage: create temp snapshot file: %w", err)
	}

	tmpName := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpName)
	}()

	if _, errWrite := tmpFile.Write(data); errWrite != nil {
		return fmt.Errorf("usage: write temp snapshot file: %w", errWrite)
	}
	if errSync := tmpFile.Sync(); errSync != nil {
		return fmt.Errorf("usage: sync temp snapshot file: %w", errSync)
	}
	if errClose := tmpFile.Close(); errClose != nil {
		return fmt.Errorf("usage: close temp snapshot file: %w", errClose)
	}
	if errRename := os.Rename(tmpName, target); errRename != nil {
		return fmt.Errorf("usage: rename snapshot file: %w", errRename)
	}

	if dirHandle, errOpenDir := os.Open(dir); errOpenDir == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}

	return nil
}
