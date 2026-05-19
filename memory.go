package main

type Interaction struct {
	User      string     `json:"user"`
	AI        string     `json:"ai"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolResponses []Message `json:"tool_responses,omitempty"`
}

func (a *App) getHistoryForModel() []Message {
	var messages []Message

	interactions := a.interactions
	if len(interactions) == 0 {
		return messages
	}

	startIdx := 0
	if a.config.RememberFirst && len(interactions) > a.config.MemoryLimit {
		// Add first interaction
		messages = append(messages, Message{Role: "user", Content: interactions[0].User})
		messages = append(messages, Message{Role: "assistant", Content: interactions[0].AI})
		startIdx = len(interactions) - (a.config.MemoryLimit - 1)
		if startIdx <= 0 {
			startIdx = 1
		}
	} else if len(interactions) > a.config.MemoryLimit {
		startIdx = len(interactions) - a.config.MemoryLimit
	}

	for i := startIdx; i < len(interactions); i++ {
		if interactions[i].User != "" {
			messages = append(messages, Message{Role: "user", Content: interactions[i].User})
		}
		assistantMsg := Message{Role: "assistant", Content: interactions[i].AI}
		if len(interactions[i].ToolCalls) > 0 {
			assistantMsg.ToolCalls = interactions[i].ToolCalls
		}
		messages = append(messages, assistantMsg)
		if len(interactions[i].ToolResponses) > 0 {
			messages = append(messages, interactions[i].ToolResponses...)
		}
	}

	return messages
}

func (a *App) addToHistory(user, ai string) {
	a.interactions = append(a.interactions, Interaction{User: user, AI: ai})
}

func (a *App) addToHistoryFull(user, ai string, toolCalls []ToolCall, toolResponses []Message) {
	a.interactions = append(a.interactions, Interaction{
		User:          user,
		AI:            ai,
		ToolCalls:     toolCalls,
		ToolResponses: toolResponses,
	})
}

func (a *App) ClearHistory() {
	a.interactions = []Interaction{}
}
