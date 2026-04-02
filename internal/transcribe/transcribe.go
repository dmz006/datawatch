// Package transcribe provides voice-to-text transcription using OpenAI Whisper.
package transcribe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Transcriber converts audio files to text.
type Transcriber interface {
	// Transcribe converts an audio file to text. Returns the transcribed text.
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

// WhisperTranscriber transcribes audio using the whisper Python package from a venv.
type WhisperTranscriber struct {
	// Python is the absolute path to the python3 executable inside the venv.
	Python string
	// Model is the whisper model size (tiny, base, small, medium, large).
	Model string
	// Language is the ISO 639-1 language code, or "" for auto-detection.
	Language string
}

// New creates a WhisperTranscriber from config values.
// venvPath is the path to the Python virtualenv (absolute or relative to cwd).
// Returns an error if the venv python or whisper package is not found.
func New(venvPath, model, language string) (*WhisperTranscriber, error) {
	if !filepath.IsAbs(venvPath) {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("transcribe: get cwd: %w", err)
		}
		venvPath = filepath.Join(cwd, venvPath)
	}
	python := filepath.Join(venvPath, "bin", "python3")
	if _, err := os.Stat(python); err != nil {
		return nil, fmt.Errorf("transcribe: python3 not found at %s — create venv with: python3 -m venv %s", python, venvPath)
	}

	// Verify whisper is importable
	check := exec.Command(python, "-c", "import whisper")
	if out, err := check.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("transcribe: whisper not installed in %s — run: %s/bin/pip install openai-whisper\n%s", venvPath, venvPath, string(out))
	}

	lang := strings.TrimSpace(language)
	if strings.EqualFold(lang, "auto") {
		lang = ""
	}

	return &WhisperTranscriber{
		Python:   python,
		Model:    model,
		Language: lang,
	}, nil
}

// whisperScript is a minimal Python script that loads whisper and transcribes an audio file.
// Arguments: audio_path model language output_path
const whisperScript = `
import sys, whisper, json
audio_path, model_name, language, output_path = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4]
model = whisper.load_model(model_name, device="cpu")
opts = {"fp16": False}
if language:
    opts["language"] = language
result = model.transcribe(audio_path, **opts)
with open(output_path, "w") as f:
    f.write(result["text"].strip())
`

// Transcribe runs whisper on the given audio file and returns the text output.
func (w *WhisperTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
	}

	// Create temp file for output
	tmpFile, err := os.CreateTemp("", "whisper-result-*.txt")
	if err != nil {
		return "", fmt.Errorf("transcribe: temp file: %w", err)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	cmd := exec.CommandContext(ctx, w.Python, "-c", whisperScript,
		audioPath, w.Model, w.Language, tmpFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("transcribe: whisper failed: %w\noutput: %s", err, string(output))
	}

	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("transcribe: read result: %w", err)
	}

	return strings.TrimSpace(string(data)), nil
}

// SupportedLanguages returns Whisper's supported language codes and names.
func SupportedLanguages() map[string]string {
	return map[string]string{
		"af": "Afrikaans", "am": "Amharic", "ar": "Arabic", "as": "Assamese",
		"az": "Azerbaijani", "ba": "Bashkir", "be": "Belarusian", "bg": "Bulgarian",
		"bn": "Bengali", "bo": "Tibetan", "br": "Breton", "bs": "Bosnian",
		"ca": "Catalan", "cs": "Czech", "cy": "Welsh", "da": "Danish",
		"de": "German", "el": "Greek", "en": "English", "es": "Spanish",
		"et": "Estonian", "eu": "Basque", "fa": "Persian", "fi": "Finnish",
		"fo": "Faroese", "fr": "French", "gl": "Galician", "gu": "Gujarati",
		"ha": "Hausa", "haw": "Hawaiian", "he": "Hebrew", "hi": "Hindi",
		"hr": "Croatian", "ht": "Haitian Creole", "hu": "Hungarian", "hy": "Armenian",
		"id": "Indonesian", "is": "Icelandic", "it": "Italian", "ja": "Japanese",
		"jw": "Javanese", "ka": "Georgian", "kk": "Kazakh", "km": "Khmer",
		"kn": "Kannada", "ko": "Korean", "la": "Latin", "lb": "Luxembourgish",
		"ln": "Lingala", "lo": "Lao", "lt": "Lithuanian", "lv": "Latvian",
		"mg": "Malagasy", "mi": "Maori", "mk": "Macedonian", "ml": "Malayalam",
		"mn": "Mongolian", "mr": "Marathi", "ms": "Malay", "mt": "Maltese",
		"my": "Myanmar", "ne": "Nepali", "nl": "Dutch", "nn": "Nynorsk",
		"no": "Norwegian", "oc": "Occitan", "pa": "Panjabi", "pl": "Polish",
		"ps": "Pashto", "pt": "Portuguese", "ro": "Romanian", "ru": "Russian",
		"sa": "Sanskrit", "sd": "Sindhi", "si": "Sinhala", "sk": "Slovak",
		"sl": "Slovenian", "sn": "Shona", "so": "Somali", "sq": "Albanian",
		"sr": "Serbian", "su": "Sundanese", "sv": "Swedish", "sw": "Swahili",
		"ta": "Tamil", "te": "Telugu", "tg": "Tajik", "th": "Thai",
		"tk": "Turkmen", "tl": "Tagalog", "tr": "Turkish", "tt": "Tatar",
		"uk": "Ukrainian", "ur": "Urdu", "uz": "Uzbek", "vi": "Vietnamese",
		"yi": "Yiddish", "yo": "Yoruba", "zh": "Chinese",
	}
}
