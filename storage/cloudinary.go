package storage

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gin-backend/config"
)

type CloudinaryClient struct {
	cloudName string
	apiKey    string
	apiSecret string
	folder    string
	client    *http.Client
}

type cloudinaryUploadResponse struct {
	SecureURL string `json:"secure_url"`
}

var invalidPublicIDChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func NewCloudinaryClient() *CloudinaryClient {
	return &CloudinaryClient{
		cloudName: config.GetCloudinaryCloudName(),
		apiKey:    config.GetCloudinaryAPIKey(),
		apiSecret: config.GetCloudinaryAPISecret(),
		folder:    config.GetCloudinaryFolder(),
		client:    &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *CloudinaryClient) Enabled() bool {
	return c != nil && c.cloudName != "" && c.apiKey != "" && c.apiSecret != ""
}

func (c *CloudinaryClient) Upload(ctx context.Context, fileName string, payload []byte) (string, error) {
	if !c.Enabled() {
		return "", fmt.Errorf("cloudinary is not configured")
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	publicID := sanitizePublicID(filepath.Base(fileName))
	if publicID == "" {
		publicID = "upload"
	}
	folder := strings.TrimSpace(c.folder)

	signatureBase := "folder=" + folder + "&public_id=" + publicID + "&timestamp=" + timestamp + c.apiSecret
	signatureHash := sha1.Sum([]byte(signatureBase))
	signature := hex.EncodeToString(signatureHash[:])

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filepath.Base(fileName))
	if err != nil {
		return "", err
	}
	if _, err := part.Write(payload); err != nil {
		return "", err
	}
	if err := writer.WriteField("api_key", c.apiKey); err != nil {
		return "", err
	}
	if err := writer.WriteField("timestamp", timestamp); err != nil {
		return "", err
	}
	if err := writer.WriteField("signature", signature); err != nil {
		return "", err
	}
	if err := writer.WriteField("folder", folder); err != nil {
		return "", err
	}
	if err := writer.WriteField("public_id", publicID); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	endpoint := fmt.Sprintf("%s/%s/auto/upload", config.GetCloudinaryBaseURL(), c.cloudName)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	responseBody := &bytes.Buffer{}
	if _, err := responseBody.ReadFrom(resp.Body); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cloudinary upload failed with status %d: %s", resp.StatusCode, responseBody.String())
	}

	var parsed cloudinaryUploadResponse
	if err := json.Unmarshal(responseBody.Bytes(), &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.SecureURL) == "" {
		return "", fmt.Errorf("cloudinary response missing secure_url")
	}

	return parsed.SecureURL, nil
}

func sanitizePublicID(fileName string) string {
	publicID := strings.TrimSpace(strings.Trim(filepath.Base(fileName), "."))
	publicID = strings.ReplaceAll(publicID, " ", "_")
	publicID = invalidPublicIDChars.ReplaceAllString(publicID, "_")
	publicID = strings.Trim(publicID, "._-")
	if publicID == "" {
		return "upload"
	}
	return publicID
}
