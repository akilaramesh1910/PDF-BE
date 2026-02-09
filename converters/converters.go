package converters

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

// LibreOffice: DOCX -> PDF, PDF -> DOCX, PPT -> PDF, XLSX -> PDF
func LibreOfficeConvert(inputPath, outputDir, toFormat string) error {
	sofficePath := "/opt/homebrew/bin/soffice"
	if _, err := os.Stat(sofficePath); os.IsNotExist(err) {
		sofficePath = "/Applications/LibreOffice.app/Contents/MacOS/soffice"
	}
	if _, err := os.Stat(sofficePath); os.IsNotExist(err) {
		sofficePath = "/usr/bin/libreoffice"
	}
	if _, err := os.Stat(sofficePath); os.IsNotExist(err) {
		sofficePath = "/usr/bin/soffice"
	}
	if _, err := os.Stat(sofficePath); os.IsNotExist(err) {
		sofficePath = "soffice" // Fallback to PATH
	}

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for output: %v", err)
	}

	absInputPath, err := filepath.Abs(inputPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for input: %v", err)
	}

	// Create a unique user installation directory to avoid locking issues in containers
	userInstallDir := filepath.Join(absOutputDir, "soffice_user")
	if err := os.MkdirAll(userInstallDir, 0755); err != nil {
		return fmt.Errorf("failed to create user installation directory: %v", err)
	}

	args := []string{
		"-env:UserInstallation=file://" + userInstallDir,
		"--headless",
		"--convert-to", toFormat,
		"--outdir", absOutputDir,
		absInputPath,
	}

	cmd := exec.Command(sofficePath, args...)
	log.Printf("Executing: %s %v", sofficePath, args)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("LibreOffice error output: %s", string(output))
		return fmt.Errorf("LibreOffice failed: %v, output: %s", err, string(output))
	}
	return nil
}

// Pandoc: TXT/MD -> PDF, HTML -> PDF
func PandocConvert(inputPath, outputPath string) error {
	args := []string{
		inputPath,
		"-o", outputPath,
	}
	cmd := exec.Command("pandoc", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Pandoc failed: %v, output: %s", err, string(output))
	}
	return nil
}

// Poppler (pdftoppm): PDF -> JPG/PNG
func PDFToImage(inputPath, outputPrefix, format string) error {
	var fmtFlag string
	if format == "jpg" || format == "jpeg" {
		fmtFlag = "-jpeg"
	} else if format == "png" {
		fmtFlag = "-png"
	} else {
		return fmt.Errorf("unsupported image format: %s", format)
	}

	args := []string{
		fmtFlag,
		inputPath,
		outputPrefix,
	}
	cmd := exec.Command("pdftoppm", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdftoppm failed: %v, output: %s", err, string(output))
	}
	return nil
}

// ImageMagick: JPG/PNG -> PDF, Multiple images -> PDF
func ImageToPDF(inputPaths []string, outputPath string) error {
	args := append(inputPaths, outputPath)
	cmd := exec.Command("convert", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ImageMagick failed: %v, output: %s", err, string(output))
	}
	return nil
}

// Poppler (pdfunite): Merge PDFs
func MergePDFs(inputPaths []string, outputPath string) error {
	args := append(inputPaths, outputPath)
	cmd := exec.Command("pdfunite", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdfunite failed: %v, output: %s", err, string(output))
	}
	return nil
}

// Poppler (pdfseparate): Split PDF
func SplitPDF(inputPath, outputPattern string) error {
	// outputPattern should be like "page-%d.pdf"
	args := []string{
		inputPath,
		outputPattern,
	}
	cmd := exec.Command("pdfseparate", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdfseparate failed: %v, output: %s", err, string(output))
	}
	return nil
}

// Ghostscript: Compress PDF
func CompressPDF(inputPath, outputPath string) error {
	args := []string{
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.4",
		"-dPDFSETTINGS=/screen", // /screen is lowest, /ebook is medium, /printer and /prepress are higher
		"-dNOPAUSE",
		"-dQUIET",
		"-dBATCH",
		"-sOutputFile=" + outputPath,
		inputPath,
	}
	cmd := exec.Command("gs", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Ghostscript failed: %v, output: %s", err, string(output))
	}
	return nil
}

// Poppler (pdftotext): Extract Text
func ExtractText(inputPath, outputPath string) error {
	args := []string{
		inputPath,
		outputPath,
	}
	cmd := exec.Command("pdftotext", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdftotext failed: %v, output: %s", err, string(output))
	}
	return nil
}

// Poppler (pdfimages): Extract Images
func ExtractImages(inputPath, outputPrefix string) error {
	args := []string{
		"-all",
		inputPath,
		outputPrefix,
	}
	cmd := exec.Command("pdfimages", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdfimages failed: %v, output: %s", err, string(output))
	}
	return nil
}

// QPDF: Rotate PDF
func RotatePDF(inputPath, outputPath string, angle int) error {
	// angle can be 90, 180, 270
	args := []string{
		inputPath,
		"--rotate=+" + fmt.Sprintf("%d", angle),
		outputPath,
	}
	cmd := exec.Command("qpdf", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qpdf rotate failed: %v, output: %s", err, string(output))
	}
	return nil
}

// QPDF: Reorder PDF
func ReorderPDF(inputPath, outputPath string, pageOrder string) error {
	// pageOrder like "1,3,2,4-last"
	args := []string{
		inputPath,
		"--pages", ".", pageOrder, "--",
		outputPath,
	}
	cmd := exec.Command("qpdf", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qpdf reorder failed: %v, output: %s", err, string(output))
	}
	return nil
}
