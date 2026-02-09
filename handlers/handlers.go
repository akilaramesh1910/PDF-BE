package handlers

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/akila/document-converter/converters"
	"github.com/akila/document-converter/models"
	"github.com/akila/document-converter/utils"
	"github.com/akila/document-converter/workers"
	"github.com/google/uuid"
)

type ConversionHandler struct {
	EngineManager *workers.EngineManager
}

func NewConversionHandler(mgr *workers.EngineManager) *ConversionHandler {
	return &ConversionHandler{EngineManager: mgr}
}

func (h *ConversionHandler) HandleConvert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Max 20MB
	r.Body = http.MaxBytesReader(w, r.Body, 20*1024*1024)
	err := r.ParseMultipartForm(20 * 1024 * 1024)
	if err != nil {
		http.Error(w, "File too large or invalid form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	from := r.FormValue("from")
	to := r.FormValue("to")

	if from == "" || to == "" {
		http.Error(w, "Missing from/to parameters", http.StatusBadRequest)
		return
	}

	// Create temp directory for this request
	reqID := uuid.New().String()
	log.Printf("[%s] Starting conversion request: %s -> %s", reqID, from, to)
	tempDir := filepath.Join("tmp", reqID)
	err = os.MkdirAll(tempDir, 0755)
	if err != nil {
		log.Printf("[%s] Failed to create temp dir: %v", reqID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	inputPath := filepath.Join(tempDir, header.Filename)
	out, err := os.Create(inputPath)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer out.Close()

	_, err = io.Copy(out, file)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Define job
	resultChan := make(chan models.JobResult, 1)
	job := models.Job{
		ID:         reqID,
		InputPath:  inputPath,
		FromFormat: from,
		ToFormat:   to,
		ResultChan: resultChan,
		TempDir:    tempDir,
	}

	// Route to correct pool
	pool := h.selectPool(from, to)
	if pool == nil {
		job.Cleanup()
		http.Error(w, "Unsupported conversion", http.StatusBadRequest)
		return
	}

	log.Printf("[%s] Job queued for %s -> %s", reqID, from, to)
	pool.JobQueue <- job

	// Wait for result
	result := <-resultChan
	if !result.Success {
		log.Printf("[%s] Conversion failed: %v", reqID, result.Error)
		job.Cleanup()
		http.Error(w, fmt.Sprintf("Conversion failed: %v", result.Error), http.StatusInternalServerError)
		return
	}

	log.Printf("[%s] Conversion successful, streaming file: %s", reqID, result.Path)

	// Stream response
	downloadFile, err := os.Open(result.Path)
	if err != nil {
		job.Cleanup()
		http.Error(w, "Failed to open result", http.StatusInternalServerError)
		return
	}
	defer downloadFile.Close()

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(result.Path)))
	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, downloadFile)

	// Final cleanup
	job.Cleanup()
}

func (h *ConversionHandler) selectPool(from, to string) *workers.WorkerPool {
	from = strings.ToLower(from)
	to = strings.ToLower(to)

	// Image conversions
	if (from == "jpg" || from == "png" || from == "jpeg") && to == "pdf" {
		return h.EngineManager.ImageMagickPool
	}
	if from == "pdf" && (to == "jpg" || to == "png" || to == "jpeg") {
		return h.EngineManager.PopplerPool
	}

	// Document conversions
	if (from == "docx" || from == "ppt" || from == "xlsx" || from == "csv" || from == "html") && to == "pdf" {
		return h.EngineManager.LibreOfficePool
	}
	if from == "pdf" && (to == "docx" || to == "xlsx" || to == "ppt") {
		return h.EngineManager.LibreOfficePool
	}

	// Text/Markdown/HTML/EPUB
	if (from == "md" || from == "markdown" || from == "epub") && to == "pdf" {
		return h.EngineManager.PandocPool
	}
	if (from == "txt" || from == "html" || from == "docx" || from == "ppt" || from == "xlsx" || from == "csv") && to == "pdf" {
		return h.EngineManager.LibreOfficePool
	}

	// PDF specific
	if from == "pdf" && to == "txt" {
		return h.EngineManager.PopplerPool
	}

	return nil
}

func (h *ConversionHandler) HandleMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(50 * 1024 * 1024) // 50MB for merge
	if err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) < 2 {
		http.Error(w, "At least 2 files required for merge", http.StatusBadRequest)
		return
	}

	reqID := uuid.New().String()
	tempDir := filepath.Join("tmp", reqID)
	os.MkdirAll(tempDir, 0755)

	var inputPaths []string
	isImageMerge := false
	for i, fileHeader := range files {
		ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
			isImageMerge = true
		}
		src, _ := fileHeader.Open()
		path := filepath.Join(tempDir, fmt.Sprintf("input_%d%s", i, ext))
		dst, _ := os.Create(path)
		io.Copy(dst, src)
		src.Close()
		dst.Close()
		inputPaths = append(inputPaths, path)
	}

	outputPath := filepath.Join(tempDir, "merged.pdf")
	if isImageMerge {
		err = h.EngineManager.ImageToPDFSync(inputPaths, outputPath)
	} else {
		err = h.EngineManager.MergePDFsSync(inputPaths, outputPath)
	}

	if err != nil {
		os.RemoveAll(tempDir)
		http.Error(w, fmt.Sprintf("Merge failed: %v", err), http.StatusInternalServerError)
		return
	}

	h.serveAndCleanup(w, outputPath, tempDir)
}

func (h *ConversionHandler) HandleCompress(w http.ResponseWriter, r *http.Request) {
	h.handleGenericPDFOperation(w, r, "compress")
}

func (h *ConversionHandler) HandleExtractText(w http.ResponseWriter, r *http.Request) {
	h.handleGenericPDFOperation(w, r, "extract-text")
}

func (h *ConversionHandler) HandleSplit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 25*1024*1024)
	if err := r.ParseMultipartForm(25 * 1024 * 1024); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	reqID := uuid.New().String()
	tempDir := filepath.Join("tmp", reqID)
	os.MkdirAll(tempDir, 0755)

	inputPath := filepath.Join(tempDir, header.Filename)
	dst, _ := os.Create(inputPath)
	io.Copy(dst, file)
	dst.Close()

	// Split PDF
	outputPattern := filepath.Join(tempDir, "page-%d.pdf")
	err = converters.SplitPDF(inputPath, outputPattern)
	if err != nil {
		os.RemoveAll(tempDir)
		http.Error(w, "Split failed", http.StatusInternalServerError)
		return
	}

	// Zip the pages
	files, _ := filepath.Glob(filepath.Join(tempDir, "page-*.pdf"))
	zipPath := filepath.Join(tempDir, "pages.zip")
	if err := utils.ZipFiles(zipPath, files); err != nil {
		os.RemoveAll(tempDir)
		http.Error(w, "Zipping failed", http.StatusInternalServerError)
		return
	}

	h.serveAndCleanup(w, zipPath, tempDir)
}

func (h *ConversionHandler) HandleExtractImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 25*1024*1024)
	if err := r.ParseMultipartForm(25 * 1024 * 1024); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	reqID := uuid.New().String()
	tempDir := filepath.Join("tmp", reqID)
	os.MkdirAll(tempDir, 0755)

	inputPath := filepath.Join(tempDir, header.Filename)
	dst, _ := os.Create(inputPath)
	io.Copy(dst, file)
	dst.Close()

	// Extract images
	outputPrefix := filepath.Join(tempDir, "img")
	err = converters.ExtractImages(inputPath, outputPrefix)
	if err != nil {
		os.RemoveAll(tempDir)
		http.Error(w, "Extraction failed", http.StatusInternalServerError)
		return
	}

	// Zip the images - pdfimages can output different formats like ppm, pbm, jpg, png based on flags
	// We'll just grab everything that's not the input file
	allFiles, _ := filepath.Glob(filepath.Join(tempDir, "*"))
	var images []string
	for _, f := range allFiles {
		if f != inputPath && !strings.HasSuffix(f, ".zip") {
			images = append(images, f)
		}
	}

	if len(images) == 0 {
		os.RemoveAll(tempDir)
		http.Error(w, "No images found in PDF", http.StatusNotFound)
		return
	}

	zipPath := filepath.Join(tempDir, "images.zip")
	if err := utils.ZipFiles(zipPath, images); err != nil {
		os.RemoveAll(tempDir)
		http.Error(w, "Zipping failed", http.StatusInternalServerError)
		return
	}

	h.serveAndCleanup(w, zipPath, tempDir)
}

func (h *ConversionHandler) HandleRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 20*1024*1024)
	if err := r.ParseMultipartForm(20 * 1024 * 1024); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	angleStr := r.FormValue("angle")
	var angle int
	fmt.Sscanf(angleStr, "%d", &angle)
	if angle == 0 {
		angle = 90 // Default
	}

	reqID := uuid.New().String()
	tempDir := filepath.Join("tmp", reqID)
	os.MkdirAll(tempDir, 0755)

	inputPath := filepath.Join(tempDir, header.Filename)
	dst, _ := os.Create(inputPath)
	io.Copy(dst, file)
	dst.Close()

	outputPath := filepath.Join(tempDir, "rotated.pdf")
	err = converters.RotatePDF(inputPath, outputPath, angle)
	if err != nil {
		os.RemoveAll(tempDir)
		http.Error(w, "Rotation failed", http.StatusInternalServerError)
		return
	}

	h.serveAndCleanup(w, outputPath, tempDir)
}

func (h *ConversionHandler) HandleReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 20*1024*1024)
	if err := r.ParseMultipartForm(20 * 1024 * 1024); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	order := r.FormValue("order") // e.g. "1,3,2"
	if order == "" {
		http.Error(w, "Missing order parameter", http.StatusBadRequest)
		return
	}

	reqID := uuid.New().String()
	tempDir := filepath.Join("tmp", reqID)
	os.MkdirAll(tempDir, 0755)

	inputPath := filepath.Join(tempDir, header.Filename)
	dst, _ := os.Create(inputPath)
	io.Copy(dst, file)
	dst.Close()

	outputPath := filepath.Join(tempDir, "reordered.pdf")
	err = converters.ReorderPDF(inputPath, outputPath, order)
	if err != nil {
		os.RemoveAll(tempDir)
		http.Error(w, "Reordering failed", http.StatusInternalServerError)
		return
	}

	h.serveAndCleanup(w, outputPath, tempDir)
}

func (h *ConversionHandler) handleGenericPDFOperation(w http.ResponseWriter, r *http.Request, op string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 25*1024*1024)
	err := r.ParseMultipartForm(25 * 1024 * 1024)
	if err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	reqID := uuid.New().String()
	tempDir := filepath.Join("tmp", reqID)
	os.MkdirAll(tempDir, 0755)

	inputPath := filepath.Join(tempDir, header.Filename)
	dst, _ := os.Create(inputPath)
	io.Copy(dst, file)
	dst.Close()

	resultChan := make(chan models.JobResult, 1)
	job := models.Job{
		ID:         reqID,
		InputPath:  inputPath,
		ToFormat:   "pdf", // Default
		ResultChan: resultChan,
		TempDir:    tempDir,
	}

	pool := h.EngineManager.PopplerPool
	if op == "compress" {
		pool = h.EngineManager.GhostscriptPool
	} else if op == "extract-text" {
		job.ToFormat = "txt"
	}

	pool.JobQueue <- job
	result := <-resultChan

	if !result.Success {
		os.RemoveAll(tempDir)
		http.Error(w, "Operation failed", http.StatusInternalServerError)
		return
	}

	h.serveAndCleanup(w, result.Path, tempDir)
}

func (h *ConversionHandler) serveAndCleanup(w http.ResponseWriter, path, tempDir string) {
	f, err := os.Open(path)
	if err != nil {
		os.RemoveAll(tempDir)
		http.Error(w, "Failed to open result", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(path)))
	io.Copy(w, f)
	os.RemoveAll(tempDir)
}
