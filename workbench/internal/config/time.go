package config

import "time"

// nowMs 返回毫秒时间戳（集中定义便于测试）。
func nowMs() int64 { return time.Now().UnixMilli() }
