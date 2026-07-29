import sys
import json

def main():
    if len(sys.argv) < 2:
        print(json.dumps({"error": "No file path provided", "text": "", "title": ""}))
        sys.exit(1)
        
    path = sys.argv[1]
    result = {"text": "", "title": "", "error": ""}
    
    try:
        from markitdown import MarkItDown
        md = MarkItDown()
        res = md.convert(path)
        result["text"] = res.text_content
    except Exception as e:
        result["error"] = str(e)
        
    print(json.dumps(result))

if __name__ == "__main__":
    main()
