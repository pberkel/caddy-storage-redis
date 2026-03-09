package storageredis

import (
	"bytes"
	"compress/flate"
	"fmt"
	"io"
)

const maxDecompressedBytes int64 = 4 << 20 // 4 MiB

func (rs *RedisStorage) compress(input []byte) ([]byte, error) {

	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		return nil, err
	}
	_, err = writer.Write(input)
	if err != nil {
		return nil, err
	}

	if err = writer.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (rs *RedisStorage) uncompress(input []byte) ([]byte, error) {

	reader := flate.NewReader(bytes.NewReader(input))
	defer reader.Close()

	limitedReader := io.LimitReader(reader, maxDecompressedBytes+1)
	output, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	if int64(len(output)) > maxDecompressedBytes {
		return nil, fmt.Errorf("decompressed value exceeds limit (%d bytes)", maxDecompressedBytes)
	}

	return output, nil
}
