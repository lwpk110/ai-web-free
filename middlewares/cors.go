package middlewares

import "github.com/gin-gonic/gin"

func Cors(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "*")
	c.Header("Access-Control-Allow-Headers", "*")
	c.Header("Access-Control-Expose-Headers", "X-TTS-Provider, X-TTS-Voice, X-TTS-Fallback, X-TTS-Fallback-Reason, X-Voice-Cache")
	c.Next()
}
