package service

import "net/http"

const (
	openAIHTTPTransportExtraKey = "openai_http_transport"
	openAIHTTPTransportHTTP1    = "http1"
)

// useOpenAIHTTP1Transport 是账号级灰度开关，仅对 OpenAI API Key 账号生效。
// 值采用严格字符串比较；缺失值、未知值、空白变体和非字符串值均保持原路径。
func useOpenAIHTTP1Transport(account *Account) bool {
	return account != nil &&
		account.Platform == PlatformOpenAI &&
		account.Type == AccountTypeAPIKey &&
		account.GetExtraString(openAIHTTPTransportExtraKey) == openAIHTTPTransportHTTP1
}

// withOpenAIAccountHTTPTransport 将命中灰度条件的账号标记到请求上下文。
// 不命中条件的请求保持原对象和原上下文不变。
func withOpenAIAccountHTTPTransport(request *http.Request, account *Account) *http.Request {
	if request == nil || !useOpenAIHTTP1Transport(account) {
		return request
	}
	return request.WithContext(WithHTTPUpstreamProfile(request.Context(), HTTPUpstreamProfileOpenAIHTTP1))
}
