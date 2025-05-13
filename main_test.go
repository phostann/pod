package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDownload(t *testing.T) {
	if err := downloadFileTest(); err != nil {
		t.Errorf("下载文件失败: %v", err)
	}
	t.Logf("下载文件成功")
}

func downloadFileTest() error {
	// 定义服务器基础URL
	baseURL := "http://localhost:8080"

	// 1. 初始化下载，获取文件信息
	downloadURL, err := url.Parse(baseURL + "/file/download/init")
	if err != nil {
		return fmt.Errorf("解析下载URL失败: %v", err)
	}

	params := url.Values{}
	params.Set("fileName", "stub.tar")
	params.Set("chunkSize", strconv.Itoa(5*1024*1024)) // 5MB
	downloadURL.RawQuery = params.Encode()

	resp, err := http.Get(downloadURL.String())
	if err != nil {
		return fmt.Errorf("下载文件初始化失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应体失败: %v", err)
	}

	// 解析下载信息
	var downloadInfo struct {
		ChunkSize   int64  `json:"chunkSize"`
		DownloadUrl string `json:"downloadUrl"`
		FileHash    string `json:"fileHash"`
		FileID      string `json:"fileID"`
		FileName    string `json:"fileName"`
		FileSize    int64  `json:"fileSize"`
		TotalChunks int    `json:"totalChunks"`
	}

	if err := json.Unmarshal(body, &downloadInfo); err != nil {
		return fmt.Errorf("解析下载信息失败: %v", err)
	}

	fmt.Printf("下载信息: %+v\n", downloadInfo)

	// 确保下载URL是绝对路径
	if !strings.HasPrefix(downloadInfo.DownloadUrl, "http") {
		downloadInfo.DownloadUrl = baseURL + downloadInfo.DownloadUrl
	}

	// 2. 创建临时目录存放分片
	tempDir := os.TempDir()
	chunkDir := filepath.Join(tempDir, fmt.Sprintf("chunks_%s", downloadInfo.FileID))
	if err := os.MkdirAll(chunkDir, 0755); err != nil {
		return fmt.Errorf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(chunkDir) // 下载完成后清理临时目录

	// 3. 创建目标文件夹
	destDir := "downloads"
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("创建目标目录失败: %v", err)
	}

	// 4. 创建目标文件并预分配空间
	destFilePath := filepath.Join(destDir, downloadInfo.FileName)
	destFile, err := os.Create(destFilePath)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %v", err)
	}

	// 预分配文件大小，避免磁盘碎片
	if err := destFile.Truncate(downloadInfo.FileSize); err != nil {
		destFile.Close()
		return fmt.Errorf("预分配文件大小失败: %v", err)
	}
	destFile.Close()

	// 5. 并行下载文件块
	wg := sync.WaitGroup{}
	wg.Add(downloadInfo.TotalChunks)

	// 限制并发数
	maxConcurrent := 10
	semaphore := make(chan struct{}, maxConcurrent)

	// 收集错误
	errChan := make(chan error, downloadInfo.TotalChunks)

	// 下载进度跟踪
	downloadedChunks := 0
	var progressMutex sync.Mutex
	startTime := time.Now()

	for i := 0; i < downloadInfo.TotalChunks; i++ {
		go func(chunkIndex int) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// 构建下载URL
			chunkURL := fmt.Sprintf("%s%d", downloadInfo.DownloadUrl, chunkIndex)

			// 下载分片
			chunkResp, err := http.Get(chunkURL)
			if err != nil {
				errChan <- fmt.Errorf("下载分片 %d 失败: %v", chunkIndex, err)
				return
			}
			defer chunkResp.Body.Close()

			// 对于分块下载，状态码200和206(Partial Content)都是有效的
			if chunkResp.StatusCode != http.StatusOK && chunkResp.StatusCode != http.StatusPartialContent {
				errChan <- fmt.Errorf("下载分片 %d 失败，状态码: %d", chunkIndex, chunkResp.StatusCode)
				return
			}

			// 将分片保存到临时文件
			chunkFilePath := filepath.Join(chunkDir, fmt.Sprintf("chunk_%d", chunkIndex))
			chunkFile, err := os.Create(chunkFilePath)
			if err != nil {
				errChan <- fmt.Errorf("创建分片文件 %d 失败: %v", chunkIndex, err)
				return
			}
			defer chunkFile.Close()

			_, err = io.Copy(chunkFile, chunkResp.Body)
			if err != nil {
				errChan <- fmt.Errorf("保存分片 %d 内容失败: %v", chunkIndex, err)
				return
			}

			// 更新下载进度
			progressMutex.Lock()
			downloadedChunks++
			progress := float64(downloadedChunks) / float64(downloadInfo.TotalChunks) * 100
			elapsedTime := time.Since(startTime).Seconds()
			speed := float64(downloadedChunks) * float64(downloadInfo.ChunkSize) / elapsedTime / 1024 / 1024
			fmt.Printf("下载进度: %.2f%% (%d/%d), 速度: %.2f MB/s\n",
				progress, downloadedChunks, downloadInfo.TotalChunks, speed)
			progressMutex.Unlock()

		}(i)
	}

	// 等待所有下载完成
	wg.Wait()
	close(errChan)

	// 检查是否有错误
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		errMsgs := ""
		for _, err := range errors {
			errMsgs += err.Error() + "\n"
		}
		return fmt.Errorf("分片下载过程中发生错误:\n%s", errMsgs)
	}

	fmt.Println("所有分片下载完成，开始合并文件...")

	// 6. 合并文件
	destFile, err = os.OpenFile(destFilePath, os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("打开目标文件失败: %v", err)
	}
	defer destFile.Close()

	// 按顺序合并分片
	for i := 0; i < downloadInfo.TotalChunks; i++ {
		chunkFilePath := filepath.Join(chunkDir, fmt.Sprintf("chunk_%d", i))
		chunkFile, err := os.Open(chunkFilePath)
		if err != nil {
			return fmt.Errorf("打开分片文件 %d 失败: %v", i, err)
		}

		// 计算分片在文件中的偏移位置
		offset := int64(i) * downloadInfo.ChunkSize

		// 将分片内容写入目标文件的正确位置
		_, err = destFile.Seek(offset, io.SeekStart)
		if err != nil {
			chunkFile.Close()
			return fmt.Errorf("定位目标文件位置失败: %v", err)
		}

		_, err = io.Copy(destFile, chunkFile)
		chunkFile.Close()
		if err != nil {
			return fmt.Errorf("合并分片 %d 失败: %v", i, err)
		}
	}

	fmt.Println("文件合并完成，校验文件完整性...")

	// 7. 验证文件完整性
	err = verifyFileIntegrityTest(destFilePath, downloadInfo.FileHash)
	if err != nil {
		return fmt.Errorf("文件完整性校验失败: %v", err)
	}

	fmt.Printf("文件 %s 下载成功，保存到 %s\n", downloadInfo.FileName, destFilePath)
	return nil
}

// 校验文件完整性
func verifyFileIntegrityTest(filePath, expectedHash string) error {
	// 打开文件
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %v", err)
	}
	defer file.Close()

	// 分块计算MD5哈希值
	hasher := md5.New()
	bufSize := 4 * 1024 * 1024 // 4MB缓冲区
	buf := make([]byte, bufSize)

	for {
		n, err := file.Read(buf)
		if n > 0 {
			hasher.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取文件失败: %v", err)
		}
	}

	// 获取哈希值
	actualHash := hex.EncodeToString(hasher.Sum(nil))

	// 比较哈希值
	if actualHash != expectedHash {
		return fmt.Errorf("文件哈希不匹配: 期望 %s, 实际 %s", expectedHash, actualHash)
	}

	return nil
}
