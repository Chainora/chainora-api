package usecases

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"chainora-api/core/constants"
	"chainora-api/core/usecases/response"

	"github.com/gin-gonic/gin"
)

const maxUploadSizeBytes = 5 * 1024 * 1024

type MediaHandler struct {
	issuer       TokenIssuer
	cloudName    string
	apiKey       string
	apiSecret    string
	uploadPreset string
	httpClient   *http.Client
}

type cloudinaryUploadResult struct {
	SecureURL string `json:"secure_url"`
	Error     struct {
		Message string `json:"message"`
	} `json:"error"`
}

type uploadImageResponse struct {
	URL string `json:"url"`
}

func NewMediaHandler(issuer TokenIssuer, cloudName, apiKey, apiSecret, uploadPreset string) *MediaHandler {
	return &MediaHandler{
		issuer:       issuer,
		cloudName:    strings.TrimSpace(cloudName),
		apiKey:       strings.TrimSpace(apiKey),
		apiSecret:    strings.TrimSpace(apiSecret),
		uploadPreset: strings.TrimSpace(uploadPreset),
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// UploadImage godoc
// @Summary Upload media image
// @Description Uploads avatar or group image to Cloudinary and returns secure URL.
// @Tags media
// @Accept multipart/form-data
// @Produce json
// @Param file formData file true "Image file"
// @Param kind formData string true "avatar or group"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Failure 401 {object} map[string]interface{}
// @Failure 403 {object} map[string]interface{}
// @Failure 500 {object} map[string]interface{}
// @Router /v1/media/upload [post]
func (h *MediaHandler) UploadImage(ctx *gin.Context) {
	if h == nil || strings.TrimSpace(h.cloudName) == "" {
		response.WriteError(ctx, fmt.Errorf("media upload unavailable: set CLOUDINARY_CLOUD_NAME or CLOUDINARY_URL: %w", constants.ErrForbidden))
		return
	}

	token, tokenErr := extractBearerToken(ctx.GetHeader("Authorization"))
	if tokenErr != nil {
		response.WriteError(ctx, tokenErr)
		return
	}

	_, address, parseErr := h.issuer.ParseAccessToken(token)
	if parseErr != nil {
		response.WriteError(ctx, parseErr)
		return
	}

	kind := strings.ToLower(strings.TrimSpace(ctx.PostForm("kind")))
	if kind != "avatar" && kind != "group" {
		response.WriteError(ctx, fmt.Errorf("invalid media kind: must be avatar or group"))
		return
	}

	fileHeader, fileErr := ctx.FormFile("file")
	if fileErr != nil {
		response.WriteError(ctx, fmt.Errorf("file is required"))
		return
	}

	if fileHeader.Size <= 0 {
		response.WriteError(ctx, fmt.Errorf("empty file is not allowed"))
		return
	}
	if fileHeader.Size > maxUploadSizeBytes {
		response.WriteError(ctx, fmt.Errorf("file too large: max %d bytes", maxUploadSizeBytes))
		return
	}

	file, openErr := fileHeader.Open()
	if openErr != nil {
		response.WriteError(ctx, fmt.Errorf("open upload file: %w", openErr))
		return
	}
	defer file.Close()

	secureURL, uploadErr := h.uploadToCloudinary(ctx.Request.Context(), file, fileHeader, kind, strings.ToLower(strings.TrimSpace(address)))
	if uploadErr != nil {
		response.WriteError(ctx, uploadErr)
		return
	}

	response.Write(ctx.Writer, response.Ok(uploadImageResponse{URL: secureURL}))
}

func (h *MediaHandler) uploadToCloudinary(
	ctx context.Context,
	file multipart.File,
	fileHeader *multipart.FileHeader,
	kind string,
	address string,
) (string, error) {
	endpoint := fmt.Sprintf("https://api.cloudinary.com/v1_1/%s/image/upload", h.cloudName)

	buffer := &bytes.Buffer{}
	writer := multipart.NewWriter(buffer)

	filename := filepath.Base(strings.TrimSpace(fileHeader.Filename))
	if filename == "" || filename == "." || filename == "/" {
		filename = "upload-image"
	}

	part, createPartErr := writer.CreateFormFile("file", filename)
	if createPartErr != nil {
		return "", fmt.Errorf("prepare upload file part: %w", createPartErr)
	}

	if _, copyErr := io.Copy(part, file); copyErr != nil {
		return "", fmt.Errorf("read upload file: %w", copyErr)
	}

	safeAddress := strings.TrimPrefix(address, "0x")
	if safeAddress == "" {
		safeAddress = "anonymous"
	}

	folder := fmt.Sprintf("chainora/%s/%s", kind, safeAddress)
	if writeErr := writer.WriteField("folder", folder); writeErr != nil {
		return "", fmt.Errorf("prepare folder field: %w", writeErr)
	}

	if h.uploadPreset != "" {
		if writeErr := writer.WriteField("upload_preset", h.uploadPreset); writeErr != nil {
			return "", fmt.Errorf("prepare upload preset: %w", writeErr)
		}
	} else {
		if h.apiKey == "" || h.apiSecret == "" {
			return "", fmt.Errorf("cloudinary credentials missing: set upload preset or api key + api secret")
		}

		timestamp := strconv.FormatInt(time.Now().Unix(), 10)
		signatureBase := fmt.Sprintf("folder=%s&timestamp=%s%s", folder, timestamp, h.apiSecret)
		hash := sha1.Sum([]byte(signatureBase))
		signature := hex.EncodeToString(hash[:])

		if writeErr := writer.WriteField("timestamp", timestamp); writeErr != nil {
			return "", fmt.Errorf("prepare timestamp field: %w", writeErr)
		}
		if writeErr := writer.WriteField("api_key", h.apiKey); writeErr != nil {
			return "", fmt.Errorf("prepare api key field: %w", writeErr)
		}
		if writeErr := writer.WriteField("signature", signature); writeErr != nil {
			return "", fmt.Errorf("prepare signature field: %w", writeErr)
		}
	}

	if closeErr := writer.Close(); closeErr != nil {
		return "", fmt.Errorf("finalize upload payload: %w", closeErr)
	}

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, buffer)
	if reqErr != nil {
		return "", fmt.Errorf("build upload request: %w", reqErr)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, callErr := h.httpClient.Do(req)
	if callErr != nil {
		return "", fmt.Errorf("upload image failed: %w", callErr)
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return "", fmt.Errorf("read upload response: %w", readErr)
	}

	parsed := cloudinaryUploadResult{}
	if unmarshalErr := json.Unmarshal(bodyBytes, &parsed); unmarshalErr != nil {
		return "", fmt.Errorf("invalid upload response: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	if resp.StatusCode >= http.StatusBadRequest {
		detail := strings.TrimSpace(parsed.Error.Message)
		if detail == "" {
			detail = strings.TrimSpace(string(bodyBytes))
		}
		if detail == "" {
			detail = fmt.Sprintf("status %d", resp.StatusCode)
		}
		return "", fmt.Errorf("cloudinary upload failed: %s", detail)
	}

	secureURL := strings.TrimSpace(parsed.SecureURL)
	if secureURL == "" {
		return "", fmt.Errorf("cloudinary upload response missing secure_url")
	}

	return secureURL, nil
}
