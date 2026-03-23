package metrics

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync"
)

const colSize = 8 // bytes per column value (int64, uint64, float64)

// partition represents a daily partition directory containing three column files:
//   - ts.col   — timestamps as int64 (Unix milliseconds), sorted ascending
//   - sid.col  — series IDs as uint64
//   - val.col  — values as float64
//
// All three files always have the same number of rows. Row N across all files
// corresponds to the same sample.
type partition struct {
	dir string
	mu  sync.RWMutex

	tsFile  *os.File
	sidFile *os.File
	valFile *os.File
}

// openPartition opens or creates a daily partition directory and its column files.
func openPartition(dir string) (*partition, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	tsFile, err := os.OpenFile(filepath.Join(dir, "ts.col"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	sidFile, err := os.OpenFile(filepath.Join(dir, "sid.col"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		tsFile.Close()
		return nil, err
	}

	valFile, err := os.OpenFile(filepath.Join(dir, "val.col"), os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		tsFile.Close()
		sidFile.Close()
		return nil, err
	}

	return &partition{
		dir:     dir,
		tsFile:  tsFile,
		sidFile: sidFile,
		valFile: valFile,
	}, nil
}

// append writes a batch of samples to the partition's column files.
// Samples must be sorted by timestamp before calling.
//
// If any column file write fails, previously written columns in the same
// batch are truncated back to their original size to keep all three files
// aligned. This prevents silent data corruption from partial writes.
func (p *partition) append(samples []sample) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(samples)
	tsBuf := make([]byte, n*colSize)
	sidBuf := make([]byte, n*colSize)
	valBuf := make([]byte, n*colSize)

	for i, s := range samples {
		off := i * colSize
		binary.LittleEndian.PutUint64(tsBuf[off:], uint64(s.ts))
		binary.LittleEndian.PutUint64(sidBuf[off:], s.seriesID)
		binary.LittleEndian.PutUint64(valBuf[off:], math.Float64bits(s.value))
	}

	// Record original sizes so we can rollback on partial failure.
	tsInfo, _ := p.tsFile.Stat()
	tsOrigSize := tsInfo.Size()
	sidInfo, _ := p.sidFile.Stat()
	sidOrigSize := sidInfo.Size()

	if _, err := p.tsFile.Write(tsBuf); err != nil {
		p.tsFile.Truncate(tsOrigSize)
		return err
	}
	if _, err := p.sidFile.Write(sidBuf); err != nil {
		p.tsFile.Truncate(tsOrigSize)
		p.sidFile.Truncate(sidOrigSize)
		return err
	}
	if _, err := p.valFile.Write(valBuf); err != nil {
		p.tsFile.Truncate(tsOrigSize)
		p.sidFile.Truncate(sidOrigSize)
		return err
	}

	return nil
}

// readAll reads every sample from the partition and returns parallel slices
// of timestamps, series IDs, and values. Returns nil slices if the partition
// is empty.
func (p *partition) readAll() ([]int64, []uint64, []float64, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Determine row count from timestamps file size.
	info, err := p.tsFile.Stat()
	if err != nil {
		return nil, nil, nil, err
	}
	count := int(info.Size()) / colSize
	if count == 0 {
		return nil, nil, nil, nil
	}

	size := count * colSize
	tsBuf := make([]byte, size)
	sidBuf := make([]byte, size)
	valBuf := make([]byte, size)

	// ReadAt does not affect the file's write position and works correctly
	// with the append-only file handles.
	if _, err := p.tsFile.ReadAt(tsBuf, 0); err != nil {
		return nil, nil, nil, err
	}
	if _, err := p.sidFile.ReadAt(sidBuf, 0); err != nil {
		return nil, nil, nil, err
	}
	if _, err := p.valFile.ReadAt(valBuf, 0); err != nil {
		return nil, nil, nil, err
	}

	ts := make([]int64, count)
	sids := make([]uint64, count)
	vals := make([]float64, count)

	for i := 0; i < count; i++ {
		off := i * colSize
		ts[i] = int64(binary.LittleEndian.Uint64(tsBuf[off:]))
		sids[i] = binary.LittleEndian.Uint64(sidBuf[off:])
		vals[i] = math.Float64frombits(binary.LittleEndian.Uint64(valBuf[off:]))
	}

	return ts, sids, vals, nil
}

// rowCount returns the number of samples stored in this partition.
func (p *partition) rowCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	info, err := p.tsFile.Stat()
	if err != nil {
		return 0
	}
	return int(info.Size()) / colSize
}

// diskBytes returns the total bytes used by this partition's column files.
func (p *partition) diskBytes() int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var total int64
	for _, f := range []*os.File{p.tsFile, p.sidFile, p.valFile} {
		if info, err := f.Stat(); err == nil {
			total += info.Size()
		}
	}
	return total
}

func (p *partition) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tsFile != nil {
		p.tsFile.Close()
	}
	if p.sidFile != nil {
		p.sidFile.Close()
	}
	if p.valFile != nil {
		p.valFile.Close()
	}
}
