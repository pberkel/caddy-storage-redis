package storageredis

import (
	"bytes"
	"compress/flate"
	"io/ioutil"
)

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

	output, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	if err = reader.Close(); err != nil {
		return nil, err
	}

	return output, nil
}
