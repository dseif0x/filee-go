//go:build !darwin

package ocr

func systemRecognizer() TextRecognizer { return nil }

func osVersion() string { return "" }
