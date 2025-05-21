package downloader

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"com.example/pod/model"
)

// FileDownloader 文件下载器
type FileDownloader struct {
	relayServerHost string
}

// NewFileDownloader 创建文件下载器
func NewFileDownloader(relayServerHost string) *FileDownloader {
	return &FileDownloader{
		relayServerHost: relayServerHost,
	}
}

// DownloadFile 直接下载文件到指定目录
func (d *FileDownloader) DownloadFile(filename string, destDir string, fileNames ...string) error {
	downloadUrl, err := url.Parse(fmt.Sprintf("http://%s/sync/download", d.relayServerHost))
	if err != nil {
		return fmt.Errorf("解析下载URL失败: %v", err)
	}

	queryParams := url.Values{}
	queryParams.Add("filename", filename)

	downloadUrl.RawQuery = queryParams.Encode()

	resp, err := http.Get(downloadUrl.String())
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %v", err)
	}
	defer resp.Body.Close()

	return nil
}

// DownloadFileInChunks 下载文件到指定目录，使用分片下载
func (d *FileDownloader) DownloadFileInChunks(filename string, destDir string, fileNames ...string) error {
	var targetFileName *string
	if len(fileNames) > 0 {
		targetFileName = &fileNames[0]
	}

	// 确定分片大小和数量
	chunkSize := int64(3 * 1024 * 1024) // 默认3MB分片大小
	parallelDownloads := 10             // 默认并行下载数

	// 使用relayServerHost构建URL
	downloadUrl, err := url.Parse(fmt.Sprintf("http://%s/file/download/init", d.relayServerHost))
	if err != nil {
		return fmt.Errorf("解析下载URL失败: %v", err)
	}

	queryParams := url.Values{}
	queryParams.Add("file_name", filename)
	queryParams.Add("chunk_size", strconv.Itoa(int(chunkSize)))
	downloadUrl.RawQuery = queryParams.Encode()

	resp, err := http.Get(downloadUrl.String())
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %v", err)
	}
	defer resp.Body.Close()

	// 反序列化为 DownloadInfo 结构体
	var downloadInfo model.DownloadInfo
	err = json.NewDecoder(resp.Body).Decode(&downloadInfo)
	if err != nil {
		return fmt.Errorf("反序列化失败: %v", err)
	}

	log.Printf("下载信息: %+v", downloadInfo)

	// 创建目标文件
	fileSize := downloadInfo.FileSize

	// 确保下载URL是绝对路径
	baseURL := fmt.Sprintf("http://%s", d.relayServerHost)
	if !strings.HasPrefix(downloadInfo.DownloadUrl, "http") {
		downloadInfo.DownloadUrl = baseURL + downloadInfo.DownloadUrl
	}

	// 创建临时目录存放分片
	tempDir := os.TempDir()
	chunkDir := filepath.Join(tempDir, fmt.Sprintf("chunks_%s", downloadInfo.FileID))
	err = os.MkdirAll(chunkDir, 0755)
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(chunkDir) // 下载完成后清理临时目录

	// 创建目标目录（如果不存在）
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		err = os.MkdirAll(destDir, 0755)
		if err != nil {
			return fmt.Errorf("创建目标目录失败: %v", err)
		}
	}

	// 确定保存文件名
	finalFileName := downloadInfo.FileName
	if targetFileName != nil && *targetFileName != "" {
		finalFileName = *targetFileName
	}

	// 创建最终文件
	destFilePath := filepath.Join(destDir, finalFileName)
	destFile, err := os.Create(destFilePath)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %v", err)
	}
	defer destFile.Close()
	// 预分配文件大小，避免磁盘碎片
	err = destFile.Truncate(fileSize)
	if err != nil {
		return fmt.Errorf("预分配文件大小失败: %v", err)
	}

	// 开始分片下载
	var wg sync.WaitGroup
	errorChan := make(chan error, downloadInfo.TotalChunks)
	semaphore := make(chan struct{}, parallelDownloads) // 限制并发数

	// 下载进度跟踪
	downloadedChunks := 0
	var downloadedMutex sync.Mutex
	startTime := time.Now()

	log.Printf("开始下载文件 %s，共 %d 个分片", downloadInfo.FileName, downloadInfo.TotalChunks)

	for i := 0; i < downloadInfo.TotalChunks; i++ {
		wg.Add(1)
		semaphore <- struct{}{} // 获取一个并发槽

		go func(chunkIndex int) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放并发槽

			chunkURL, err := url.Parse(fmt.Sprintf("http://%s/file/download/chunk", d.relayServerHost))
			if err != nil {
				errorChan <- fmt.Errorf("解析服务器URL失败: %v", err)
				return
			}

			params := url.Values{}
			params.Add("file_id", downloadInfo.FileID)
			params.Add("chunk_index", strconv.Itoa(chunkIndex))

			chunkURL.RawQuery = params.Encode()

			// 下载分片
			chunkResp, err := http.Get(chunkURL.String())
			if err != nil {
				errorChan <- fmt.Errorf("下载分片 %d 失败: %v", chunkIndex, err)
				return
			}
			defer chunkResp.Body.Close()

			// 对于分块下载，状态码200和206(Partial Content)都是有效的
			if chunkResp.StatusCode != http.StatusOK && chunkResp.StatusCode != http.StatusPartialContent {
				errorChan <- fmt.Errorf("下载分片 %d 失败，状态码: %d", chunkIndex, chunkResp.StatusCode)
				return
			}

			// 将分片保存到临时文件
			chunkFilePath := filepath.Join(chunkDir, fmt.Sprintf("chunk_%d", chunkIndex))
			chunkFile, err := os.Create(chunkFilePath)
			if err != nil {
				errorChan <- fmt.Errorf("创建分片文件 %d 失败: %v", chunkIndex, err)
				return
			}
			defer chunkFile.Close()

			_, err = io.Copy(chunkFile, chunkResp.Body)
			if err != nil {
				errorChan <- fmt.Errorf("保存分片 %d 内容失败: %v", chunkIndex, err)
				return
			}

			// 更新下载进度
			downloadedMutex.Lock()
			downloadedChunks++
			progress := float64(downloadedChunks) / float64(downloadInfo.TotalChunks) * 100
			elapsedTime := time.Since(startTime).Seconds()
			speed := float64(downloadedChunks) * float64(downloadInfo.ChunkSize) / elapsedTime / 1024 / 1024
			log.Printf("下载进度: %.2f%% (%d/%d), 速度: %.2f MB/s",
				progress, downloadedChunks, downloadInfo.TotalChunks, speed)
			downloadedMutex.Unlock()
		}(i)
	}

	// 等待所有下载完成
	wg.Wait()
	close(errorChan)

	// 检查是否有错误
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		errMsgs := ""
		for _, err := range errors {
			errMsgs += err.Error() + "\n"
		}
		return fmt.Errorf("分片下载过程中发生错误:\n%s", errMsgs)
	}

	log.Printf("所有分片下载完成，开始合并文件...")

	// 下载完毕，合并文件
	destFile, err = os.OpenFile(destFilePath, os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("打开目标文件失败: %v", err)
	}
	defer destFile.Close()

	// 按顺序合并分片
	chunkSizeInt := downloadInfo.ChunkSize

	for i := 0; i < downloadInfo.TotalChunks; i++ {
		chunkFilePath := filepath.Join(chunkDir, fmt.Sprintf("chunk_%d", i))
		chunkFile, err := os.Open(chunkFilePath)
		if err != nil {
			return fmt.Errorf("打开分片文件 %d 失败: %v", i, err)
		}

		// 计算分片在文件中的偏移位置
		offset := int64(i) * chunkSizeInt

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

	log.Printf("文件合并完成，开始验证文件完整性...")

	// 验证文件完整性
	err = d.VerifyFileIntegrity(destFilePath, downloadInfo.FileHash)
	if err != nil {
		return fmt.Errorf("文件完整性校验失败: %v", err)
	}

	log.Printf("文件 %s 下载并验证成功", downloadInfo.FileName)
	return nil
}

// VerifyFileIntegrity 验证文件完整性
func (d *FileDownloader) VerifyFileIntegrity(filePath, expectedHash string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 分块计算哈希值
	hash := md5.New()
	bufSize := 4 * 1024 * 1024 // 4MB缓冲区
	buf := make([]byte, bufSize)

	for {
		n, err := file.Read(buf)
		if n > 0 {
			hash.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取文件失败: %v", err)
		}
	}

	actualHash := hex.EncodeToString(hash.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("哈希值不匹配，预期: %s, 实际: %s", expectedHash, actualHash)
	}

	return nil
}

// DetectNetworkSpeed 检测网络速度（简化版）
func (d *FileDownloader) DetectNetworkSpeed() int64 {
	// 这里是一个简化的网络速度检测
	// 在实际应用中，可能需要更复杂的逻辑来获取真实网速

	// 下载一个小文件来测试速度
	startTime := time.Now()
	resp, err := http.Get(fmt.Sprintf("http://%s/v1/file/speedtest", d.relayServerHost))
	if err != nil {
		log.Printf("网络速度检测失败: %v", err)
		return 0
	}
	defer resp.Body.Close()

	// 读取响应内容
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("读取速度测试响应失败: %v", err)
		return 0
	}

	// 计算速度（字节/秒）
	duration := time.Since(startTime).Seconds()
	if duration > 0 {
		speed := int64(float64(len(data)) / duration)
		return speed
	}

	return 0
}
