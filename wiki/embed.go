package wiki

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

const (
	embeddingCacheMagic         = "CPEM"
	embeddingCacheSchemaVersion = uint16(2)
	EmbeddingInputVersion       = uint16(2)
)

var ErrIncompatibleEmbeddingCache = errors.New("incompatible embedding cache")

type embedInput struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type embedOutput struct {
	ID     string    `json:"id"`
	Vector []float64 `json:"vector"`
}

// EmbeddingEntry binds a vector to the exact source bytes and semantic-input
// version that produced it. Graph lookup remains keyed by stable component ID.
type EmbeddingEntry struct {
	ID           string
	ContentHash  [sha256.Size]byte
	InputVersion uint16
	Vector       []float32
}

// ComputeEmbeddingEntries runs the shared Bun embedding script over the
// deterministic semantic projection of every component.
func ComputeEmbeddingEntries(components []Component, scriptPath string) (map[string]EmbeddingEntry, error) {
	if len(components) == 0 {
		return map[string]EmbeddingEntry{}, nil
	}

	input := make([]embedInput, 0, len(components))
	fingerprints := make(map[string][sha256.Size]byte, len(components))
	for _, component := range components {
		text, fingerprint := componentEmbeddingMaterial(component)
		input = append(input, embedInput{ID: component.ID, Text: text})
		fingerprints[component.ID] = fingerprint
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("bun", scriptPath)
	cmd.Stdin = bytes.NewReader(inputJSON)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("embed script failed: %w", err)
	}

	var output []embedOutput
	if err := json.Unmarshal(out, &output); err != nil {
		return nil, fmt.Errorf("embed output parse failed: %w", err)
	}
	result := make(map[string]EmbeddingEntry, len(output))
	for _, item := range output {
		vector := make([]float32, len(item.Vector))
		for i, value := range item.Vector {
			vector[i] = float32(value)
		}
		result[item.ID] = EmbeddingEntry{
			ID:           item.ID,
			ContentHash:  fingerprints[item.ID],
			InputVersion: EmbeddingInputVersion,
			Vector:       vector,
		}
	}
	return result, nil
}

// ComputeEmbeddings is used for ephemeral query vectors. Durable index builds
// use ComputeEmbeddingEntries so cache validity metadata is not discarded.
func ComputeEmbeddings(components []Component, scriptPath string) (map[string][]float32, error) {
	entries, err := ComputeEmbeddingEntries(components, scriptPath)
	if err != nil {
		return nil, err
	}
	return EmbeddingVectors(entries), nil
}

func componentEmbeddingMaterial(component Component) (string, [sha256.Size]byte) {
	content, fingerprint := componentContentAndFingerprint(component)
	semantic := ExtractSemanticText(component.Title, content, defaultSemanticRuneBudget)
	return semantic.Text, fingerprint
}

func componentContentAndFingerprint(component Component) ([]byte, [sha256.Size]byte) {
	content, err := os.ReadFile(component.Path)
	if component.Path == "" || err != nil {
		content = nil
	}
	fingerprintInput := append([]byte(component.Title+"\x00"), content...)
	return content, sha256.Sum256(fingerprintInput)
}

func EmbeddingFingerprint(component Component) [sha256.Size]byte {
	_, fingerprint := componentContentAndFingerprint(component)
	return fingerprint
}

func EmbeddingEntryMatches(component Component, entry EmbeddingEntry) bool {
	return entry.InputVersion == EmbeddingInputVersion && entry.ContentHash == EmbeddingFingerprint(component)
}

func EmbeddingVectors(entries map[string]EmbeddingEntry) map[string][]float32 {
	vectors := make(map[string][]float32, len(entries))
	for id, entry := range entries {
		vectors[id] = entry.Vector
	}
	return vectors
}

// SaveEmbeddingCache atomically writes a versioned cache. Stable ID ordering
// makes identical cache contents byte-for-byte reproducible.
func SaveEmbeddingCache(path string, entries map[string]EmbeddingEntry) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		temp.Close()
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	if _, err = temp.Write([]byte(embeddingCacheMagic)); err != nil {
		return err
	}
	if err = binary.Write(temp, binary.LittleEndian, embeddingCacheSchemaVersion); err != nil {
		return err
	}
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if err = binary.Write(temp, binary.LittleEndian, uint32(len(ids))); err != nil {
		return err
	}
	for _, id := range ids {
		entry := entries[id]
		idBytes := []byte(id)
		if err = binary.Write(temp, binary.LittleEndian, uint32(len(idBytes))); err != nil {
			return err
		}
		if _, err = temp.Write(idBytes); err != nil {
			return err
		}
		if _, err = temp.Write(entry.ContentHash[:]); err != nil {
			return err
		}
		if err = binary.Write(temp, binary.LittleEndian, entry.InputVersion); err != nil {
			return err
		}
		if len(entry.Vector) > int(^uint16(0)) {
			return fmt.Errorf("embedding vector too large for %s", id)
		}
		if err = binary.Write(temp, binary.LittleEndian, uint16(len(entry.Vector))); err != nil {
			return err
		}
		for _, value := range entry.Vector {
			if err = binary.Write(temp, binary.LittleEndian, value); err != nil {
				return err
			}
		}
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}

func LoadEmbeddingCache(path string) (map[string]EmbeddingEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	magic := make([]byte, len(embeddingCacheMagic))
	if _, err := io.ReadFull(file, magic); err != nil || string(magic) != embeddingCacheMagic {
		return nil, ErrIncompatibleEmbeddingCache
	}
	var schema uint16
	if err := binary.Read(file, binary.LittleEndian, &schema); err != nil || schema != embeddingCacheSchemaVersion {
		return nil, ErrIncompatibleEmbeddingCache
	}
	var count uint32
	if err := binary.Read(file, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	if count > 1_000_000 {
		return nil, fmt.Errorf("embedding cache entry count %d is invalid", count)
	}

	entries := make(map[string]EmbeddingEntry, count)
	for range count {
		var idLength uint32
		if err := binary.Read(file, binary.LittleEndian, &idLength); err != nil {
			return nil, err
		}
		if idLength == 0 || idLength > 1<<20 {
			return nil, fmt.Errorf("embedding cache id length %d is invalid", idLength)
		}
		idBytes := make([]byte, idLength)
		if _, err := io.ReadFull(file, idBytes); err != nil {
			return nil, err
		}
		entry := EmbeddingEntry{ID: string(idBytes)}
		if _, err := io.ReadFull(file, entry.ContentHash[:]); err != nil {
			return nil, err
		}
		if err := binary.Read(file, binary.LittleEndian, &entry.InputVersion); err != nil {
			return nil, err
		}
		var vectorLength uint16
		if err := binary.Read(file, binary.LittleEndian, &vectorLength); err != nil {
			return nil, err
		}
		if vectorLength == 0 || vectorLength > 4096 {
			return nil, fmt.Errorf("embedding vector length %d is invalid for %s", vectorLength, entry.ID)
		}
		entry.Vector = make([]float32, vectorLength)
		for i := range entry.Vector {
			if err := binary.Read(file, binary.LittleEndian, &entry.Vector[i]); err != nil {
				return nil, err
			}
		}
		entries[entry.ID] = entry
	}
	return entries, nil
}
