package main

type Interaction struct {
	User string `json:"user"`
	AI   string `json:"ai"`
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
		messages = append(messages, Message{Role: "user", Content: interactions[i].User})
		messages = append(messages, Message{Role: "assistant", Content: interactions[i].AI})
	}

	return messages
}

func (a *App) addToHistory(user, ai string) {
	a.interactions = append(a.interactions, Interaction{User: user, AI: ai})
}

func (a *App) ClearHistory() {
	a.interactions = []Interaction{}
}
