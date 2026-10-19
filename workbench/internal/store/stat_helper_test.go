package store

import "os"

// fileSize 返回文件字节数（测试辅助）。
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}
