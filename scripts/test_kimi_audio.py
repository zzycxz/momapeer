"""Test moonshotai/kimi-k2.6 audio recognition on MoMA platform.

Usage:
    python scripts/test_kimi_audio.py [path_to_audio_file]

If no audio file is provided, generates a minimal WAV with spoken digits
(using pyttsx3 if available, or a silent WAV as fallback).
"""

import requests
import os
import sys
import base64
import struct
import io
import json


def generate_silent_wav(duration_sec=1, sample_rate=16000):
    """Generate a minimal WAV file with silence (for testing API accepts audio)."""
    num_samples = sample_rate * duration_sec
    # 16-bit mono PCM
    data_size = num_samples * 2
    buf = io.BytesIO()
    # RIFF header
    buf.write(b"RIFF")
    buf.write(struct.pack("<I", 36 + data_size))
    buf.write(b"WAVE")
    # fmt chunk
    buf.write(b"fmt ")
    buf.write(struct.pack("<I", 16))  # chunk size
    buf.write(struct.pack("<HHIIHH", 1, 1, sample_rate, sample_rate * 2, 2, 16))
    # data chunk
    buf.write(b"data")
    buf.write(struct.pack("<I", data_size))
    buf.write(b"\x00" * data_size)
    return buf.getvalue()


def generate_tts_wav(text="1 2 3 4 5", sample_rate=16000):
    """Generate WAV with TTS speech if pyttsx3 is available."""
    try:
        import pyttsx3
        engine = pyttsx3.init()
        engine.setProperty("rate", 150)
        buf = io.BytesIO()
        engine.save_to_file(text, buf.name if hasattr(buf, "name") else "/tmp/tts_test.wav")
        engine.runAndWait()
        with open("/tmp/tts_test.wav", "rb") as f:
            return f.read()
    except Exception:
        return None


def audio_to_data_url(audio_bytes, fmt="wav"):
    """Convert audio bytes to base64 data URL."""
    b64 = base64.b64encode(audio_bytes).decode()
    return f"data:audio/{fmt};base64,{b64}"


def test_audio_recognition(audio_bytes, audio_format="wav", model="moonshotai/kimi-k2.6"):
    """Test audio recognition via MoMA API."""
    api_key = os.getenv("JIUTIAN_API_KEY")
    if not api_key:
        print("ERROR: JIUTIAN_API_KEY environment variable not set")
        sys.exit(1)

    url = "https://jiutian.10086.cn/largemodel/moma/api/v3/chat/completions"
    data_url = audio_to_data_url(audio_bytes, audio_format)

    # Try OpenAI-style input_audio format
    payload = {
        "model": model,
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "type": "text",
                        "text": "请识别这段音频中的内容，逐字转录。",
                    },
                    {
                        "type": "input_audio",
                        "input_audio": {
                            "data": base64.b64encode(audio_bytes).decode(),
                            "format": audio_format,
                        },
                    },
                ],
            }
        ],
        "stream": False,
    }

    headers = {
        "Authorization": f"Bearer {api_key}",
        "Content-Type": "application/json",
    }

    print(f"=== Test 1: input_audio type (OpenAI format) ===")
    print(f"Model: {model}")
    print(f"Audio: {audio_format}, {len(audio_bytes)} bytes")
    print(f"Sending request...\n")

    try:
        resp = requests.post(url, json=payload, headers=headers, timeout=120)
        print(f"Status: {resp.status_code}")
        result = resp.json()
        print(f"Response:\n{json.dumps(result, ensure_ascii=False, indent=2)}")
    except Exception as e:
        print(f"Error: {e}")
        result = None

    # Also try image_url style (some APIs accept audio this way)
    print(f"\n=== Test 2: image_url style with audio data URL ===")
    payload2 = {
        "model": model,
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "type": "text",
                        "text": "请识别这段音频中的内容，逐字转录。",
                    },
                    {
                        "type": "image_url",
                        "image_url": {
                            "url": data_url,
                        },
                    },
                ],
            }
        ],
        "stream": False,
    }

    try:
        resp2 = requests.post(url, json=payload2, headers=headers, timeout=120)
        print(f"Status: {resp2.status_code}")
        result2 = resp2.json()
        print(f"Response:\n{json.dumps(result2, ensure_ascii=False, indent=2)}")
    except Exception as e:
        print(f"Error: {e}")

    # Test 3: plain text with audio_url field (Moonshot specific?)
    print(f"\n=== Test 3: audio_url content type ===")
    payload3 = {
        "model": model,
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "type": "text",
                        "text": "请识别这段音频中的内容，逐字转录。",
                    },
                    {
                        "type": "audio_url",
                        "audio_url": {
                            "url": data_url,
                        },
                    },
                ],
            }
        ],
        "stream": False,
    }

    try:
        resp3 = requests.post(url, json=payload3, headers=headers, timeout=120)
        print(f"Status: {resp3.status_code}")
        result3 = resp3.json()
        print(f"Response:\n{json.dumps(result3, ensure_ascii=False, indent=2)}")
    except Exception as e:
        print(f"Error: {e}")


def main():
    if len(sys.argv) > 1:
        audio_path = sys.argv[1]
        print(f"Reading audio file: {audio_path}")
        with open(audio_path, "rb") as f:
            audio_bytes = f.read()
        ext = os.path.splitext(audio_path)[1].lstrip(".")
        if ext in ("wav", "mp3", "ogg", "flac", "m4a", "webm"):
            audio_format = ext
        else:
            audio_format = "wav"
    else:
        print("No audio file provided, generating silent WAV for API acceptance test...")
        audio_bytes = generate_silent_wav(duration_sec=2)
        audio_format = "wav"
        # Save for reference
        with open("test_silent.wav", "wb") as f:
            f.write(audio_bytes)
        print("Saved: test_silent.wav")

    test_audio_recognition(audio_bytes, audio_format)


if __name__ == "__main__":
    main()
