## gemini-image finger:

- 请求示例：

```bash
curl -X POST "https://api.bananarouter.com/v1beta/models/gemini-3.1-flash-image-preview:generateContent" \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {
        "parts": [
          { "text": "生成一座夜晚的未来赛博朋克城市." }
        ]
      }
    ],
    "generationConfig": {
      "responseModalities": ["IMAGE"],
      "imageConfig": {
        "aspectRatio": "1:1",
        "imageSize": "512"
      }
    }
  }'
```


- 响应示例：

```json
{
  "candidates": [
    {
      "content": {
        "parts": [
          {
            "inlineData": {
              "mimeType": "image/jpeg",
              "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
			  },
            "thoughtSignature": "<large string>"
			}
        ],
        "role": "model"
      },
      "finishReason": "STOP",
      "index": 0
    }
  ],
  "usageMetadata": {
    "promptTokenCount": 11,
    "candidatesTokenCount": 1135,
    "totalTokenCount": 1146,
    "promptTokensDetails": [
      {
        "modality": "TEXT",
        "tokenCount": 11
      }
    ],
    "candidatesTokensDetails": [
      {
        "modality": "IMAGE",
        "tokenCount": 747
      }
    ],
    "serviceTier": "standard"
  },
  "modelVersion": "gemini-3.1-flash-image-preview",
  "responseId": "fzgBaoLTBK2fz7IP1JbRoAQ"
}
```



### 核心响应
  `$.candidates[0].content.parts[0].inlineData.data` 这是返回的base64图片，请转为html标签显示，考虑本地化保存成文件；

### 调参
  - 输入可调参数：imageConfig.imageConfig,  imageConfig.imageSize

  