package hub

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type QueuedUpload struct {
	AudioPath string       `json:"audio_path"`
	Fields    UploadFields `json:"fields"`
	QueuedAt  time.Time    `json:"queued_at"`
	Attempts  int          `json:"attempts"`
}

type Queue struct {
	dir string
}

func NewQueue(dir string) (*Queue, error) {
	if dir == "" {
		dir = "upload-queue"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Queue{dir: dir}, nil
}

func (q *Queue) Enqueue(audioPath string, fields UploadFields) error {
	item := QueuedUpload{
		AudioPath: audioPath,
		Fields:    fields,
		QueuedAt:  time.Now(),
	}
	data, err := json.Marshal(item)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), filepath.Base(audioPath))
	tmp := filepath.Join(q.dir, name+".tmp")
	final := filepath.Join(q.dir, name)
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

func (q *Queue) Drain(client *Client) (int, error) {
	entries, err := os.ReadDir(q.dir)
	if err != nil {
		return 0, err
	}
	uploaded := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(q.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var item QueuedUpload
		if err := json.Unmarshal(data, &item); err != nil {
			continue
		}
		if err := client.UploadFile(item.AudioPath, item.Fields); err != nil {
			item.Attempts++
			_ = os.WriteFile(path, mustJSON(item), 0o644)
			continue
		}
		_ = os.Remove(path)
		uploaded++
	}
	return uploaded, nil
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
