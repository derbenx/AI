import {GetSpecs, GetConfig, SaveSettings, SendMessage, ClearHistory, GetImageBase64, GetFileContent, ListAvailableTools, SendCodeMessage, UpdateTodoList, SetCodeActive, GetAINotes, UpdateAINotes, GetDefaultCodePrompt, TestTool, StartNewSession, GetBotModel} from '../wailsjs/go/main/App';
import {EventsOn, BrowserOpenURL} from '../wailsjs/runtime/runtime';

let currentImagePath = "";

function showNotification(message, duration = 3000) {
    const el = document.getElementById('notification');
    el.textContent = message;
    el.classList.add('show');
    setTimeout(() => {
        el.classList.remove('show');
    }, duration);
}

// Tab switching
window.showTab = function(element, tabName) {
    document.querySelectorAll('.tab-content').forEach(tab => tab.classList.remove('active'));
    document.querySelectorAll('.tab-btn').forEach(btn => btn.classList.remove('active'));

    document.getElementById(`${tabName}-tab`).classList.add('active');
    element.classList.add('active');
}

// Drag & Drop
window.addEventListener('dragover', (e) => e.preventDefault());
window.addEventListener('drop', (e) => e.preventDefault());

EventsOn('file-dropped', (filePath) => {
    handleFilePath(filePath);
});

async function handleFilePath(filePath) {
    currentImagePath = filePath;
    const lowerPath = filePath.toLowerCase();
    const fileName = filePath.split('\\').pop().split('/').pop();

    if (lowerPath.endsWith('.jpg') || lowerPath.endsWith('.jpeg') || lowerPath.endsWith('.png') || lowerPath.endsWith('.webp')) {
        try {
            const base64Data = await GetImageBase64(filePath);
            const img = document.getElementById('image-preview');
            img.src = base64Data;
            img.style.display = 'block';
            document.getElementById('code-preview').style.display = 'none';
            document.getElementById('drop-zone').querySelector('p').textContent = `Loaded: ${fileName}`;
        } catch (err) {
            console.error(err);
        }
    } else {
        try {
            const content = await GetFileContent(filePath);
            const code = document.getElementById('code-preview');
            code.textContent = content.substring(0, 5000) + (content.length > 5000 ? "\n..." : "");
            code.style.display = 'block';
            document.getElementById('image-preview').style.display = 'none';
            document.getElementById('drop-zone').querySelector('p').textContent = `Loaded: ${fileName}`;
        } catch (err) {
            console.error(err);
        }
    }
}

// Sidebar Resize
const handle = document.getElementById('resize-handle');
const sidebar = document.getElementById('sidebar-container');
let isResizing = false;

handle.addEventListener('mousedown', (e) => {
    isResizing = true;
    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', () => {
        isResizing = false;
        document.removeEventListener('mousemove', handleMouseMove);
    });
});

function handleMouseMove(e) {
    if (!isResizing) return;
    const offsetRight = document.body.offsetWidth - e.clientX;
    if (offsetRight > 100 && offsetRight < 600) {
        sidebar.style.width = offsetRight + 'px';
    }
}

// Chat logic
const chatWindow = document.getElementById('chat-window');
const chatInput = document.getElementById('chat-input');
const sendBtn = document.getElementById('send-btn');

function appendMessage(role, content, reasoning = "") {
    const div = document.createElement('div');
    div.className = `message ${role}`;
    div.id = role === 'ai' ? 'latest-ai-msg' : '';

    let label = "";
    if (role === 'user') label = "(User) ";
    else if (role === 'ai') label = "(AI) ";
    else if (role === 'tool') label = "(Tool) ";

    // Avoid double labeling if content already has it
    if (content.startsWith(label)) {
        label = "";
    }

    marked.setOptions({
        breaks: true,
        gfm: true
    });

    let displayContent = label + content;
    const showThinking = document.getElementById('toggle-thinking').checked;

    if (role === 'ai') {
        if (content.startsWith('**[')) {
            // bot-message from triggers already has a label
        } else if (!label) {
            label = `(${currentTargetBot}) `;
            displayContent = label + content;
        }

        const thinkingClass = showThinking ? "thinking-block" : "thinking-block hidden";

        let combinedReasoning = reasoning;
        if (displayContent.includes('<think>')) {
            const match = displayContent.match(/<think>([\s\S]*?)(?:<\/think>|$)/);
            if (match) {
                combinedReasoning = (combinedReasoning ? combinedReasoning + "\n" : "") + match[1];
                displayContent = displayContent.replace(/<think>[\s\S]*?<\/think>/g, "");
                if (displayContent.includes('<think>')) displayContent = displayContent.split('<think>')[0];
            }
        }

        if (combinedReasoning) {
            if (showThinking) {
                displayContent = label + `<div class="${thinkingClass}">*(thinking)* ${combinedReasoning}</div>` + displayContent.substring(label.length);
            } else {
                displayContent = label + `<div style="font-style: italic; color: #888; font-size: 0.8em; margin-bottom: 5px;">-done thinking-</div>` + displayContent.substring(label.length);
            }
        }
    }

    div.innerHTML = marked.parse(displayContent);

    // Conditional auto-scroll
    const isAtBottom = chatWindow.scrollHeight - chatWindow.scrollTop <= chatWindow.clientHeight + 50;

    chatWindow.appendChild(div);

    if (isAtBottom) {
        chatWindow.scrollTop = chatWindow.scrollHeight;
    }
    return div;
}

let activeAiMessages = {}; // botName -> { div, content }
let currentTargetBot = "Main";

EventsOn('token', (data) => {
    const bot = data.bot;
    const token = data.token || "";
    const reasoning = data.reasoning || "";

    if (!activeAiMessages[bot]) {
        const oldTarget = currentTargetBot;
        currentTargetBot = bot;
        activeAiMessages[bot] = {
            div: appendMessage('ai', ''),
            content: "",
            reasoning: "",
            isThinking: false,
            wasThinking: false
        };
        currentTargetBot = oldTarget;
    }

    const msg = activeAiMessages[bot];
    msg.content += token;
    msg.reasoning += reasoning;

    const currentlyThinking = reasoning !== "" || (msg.content.includes('<think>') && !msg.content.includes('</think>'));
    if (currentlyThinking) {
        msg.isThinking = true;
        msg.wasThinking = true;
        updateBotStatus(bot, 'thinking');
    } else {
        msg.isThinking = false;
        updateBotStatus(bot, 'typing');
    }

    marked.setOptions({
        breaks: true,
        gfm: true
    });

    const isAtBottom = chatWindow.scrollHeight - chatWindow.scrollTop <= chatWindow.clientHeight + 50;
    const showThinking = document.getElementById('toggle-thinking').checked;

    let displayContent = `(${bot}) `;

    // Add reasoning block
    if (msg.reasoning || msg.content.includes('<think>')) {
        const thinkingClass = showThinking ? "thinking-block" : "thinking-block hidden";
        let rText = msg.reasoning;

        // Extract from think tags if present in content
        if (msg.content.includes('<think>')) {
            const match = msg.content.match(/<think>([\s\S]*?)(?:<\/think>|$)/);
            if (match) rText += match[1];
        }

        if (showThinking) {
            displayContent += `<div class="${thinkingClass}">*(thinking)* ${rText}</div>`;
        } else {
            const label = msg.isThinking ? "-thinking-" : "-done thinking-";
            displayContent += `<div class="thinking-block hidden">${label}</div>`; // Hidden but present
            displayContent += `<div style="font-style: italic; color: #888; font-size: 0.8em; margin-bottom: 5px;">${label}</div>`;
        }
    }

    // Add main content (stripping think tags for clean display)
    let cleanContent = msg.content.replace(/<think>[\s\S]*?<\/think>/g, "");
    if (cleanContent.includes('<think>')) {
        cleanContent = cleanContent.split('<think>')[0];
    }
    displayContent += cleanContent;

    msg.div.innerHTML = marked.parse(displayContent);

    if (isAtBottom) {
        chatWindow.scrollTop = chatWindow.scrollHeight;
    }
});

EventsOn('done', (data) => {
    const bot = data.bot;
    updateBotStatus(bot, 'idle');
    delete activeAiMessages[bot];
});

EventsOn('code-finished', (msg) => {
    appendMessage('ai', `**${msg}**`);
    stopCodeMode();
});

EventsOn('notes-updated', (notes) => {
    document.getElementById('ai-notes').value = notes;
});

EventsOn('system-prompt-display', (prompt) => {
    appendMessage('ai', `***System Prompt Sent:***\n\n${prompt}`);
});

EventsOn('todo-updated', (todo) => {
    document.getElementById('todo-list').value = todo;
});

EventsOn('internal-user-message', (msg) => {
    appendMessage('user', msg);
});

EventsOn('internal-tool-message', (msg) => {
    appendMessage('tool', msg);
});

EventsOn('bot-message', (data) => {
    appendMessage('ai', `**[${data.name}]** ${data.content}`, data.reasoning);
    updateBotStatus(data.name, 'idle');
});

sendBtn.onclick = async () => {
    const text = chatInput.value.trim();
    if (!text && !currentImagePath) return;

    if (text.startsWith('@')) {
        currentTargetBot = text.split(' ')[0].substring(1);
    } else {
        currentTargetBot = "Main";
    }

    appendMessage('user', text);
    chatInput.value = "";
    chatInput.focus();

    updateBotStatus(currentTargetBot, 'thinking');

    try {
        await SendMessage(text, currentImagePath);
    } catch (err) {
        appendMessage('ai', `Error: ${err}`);
        updateBotStatus(currentTargetBot, 'offline');
    }
};

chatInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        sendBtn.click();
    }
});

document.addEventListener('keydown', (e) => {
    if (e.ctrlKey && e.key === 'Tab') {
        e.preventDefault();
        const tabs = Array.from(document.querySelectorAll('.tab-btn'));
        const activeIdx = tabs.findIndex(t => t.classList.contains('active'));
        let nextIdx;
        if (e.shiftKey) {
            nextIdx = (activeIdx - 1 + tabs.length) % tabs.length;
        } else {
            nextIdx = (activeIdx + 1) % tabs.length;
        }
        tabs[nextIdx].click();
    }
});

document.getElementById('clear-btn').onclick = async () => {
    await ClearHistory();
    chatWindow.innerHTML = "";
};

document.getElementById('toggle-thinking').onchange = () => {
    const show = document.getElementById('toggle-thinking').checked;
    document.querySelectorAll('.thinking-block').forEach(el => {
        if (show) el.classList.remove('hidden');
        else el.classList.add('hidden');
    });
};

// Settings logic
async function initSettings() {
    const specs = await GetSpecs();
    if (specs.cpu || specs.ram > 0) {
        document.getElementById('system-specs').style.display = 'block';
        document.getElementById('spec-cpu').textContent = specs.cpu || 'Unknown';
        document.getElementById('spec-ram').textContent = specs.ram;
        document.getElementById('spec-gpu').textContent = specs.gpu || 'None Detected';
        document.getElementById('spec-vram').textContent = specs.vram;
    }

    const config = await GetConfig();
    document.getElementById('memory-limit').value = config.memory_limit;
    document.getElementById('remember-first').checked = config.remember_first;
    document.getElementById('debug-log').checked = config.debug_log;
    document.getElementById('code-prompt').value = config.code_prompt || "";

    await refreshTools();
    await initBots(config.bots);
    await initCodeSetup();
}

async function updateBotStatus(botName, status) {
    const statusEl = document.querySelector(`.bot-item[data-name="${botName}"] .bot-status`);
    if (statusEl) {
        statusEl.className = `bot-status ${status}`;
    }
}

async function initBots(bots) {
    const mainContainer = document.getElementById('main-bot-container');
    const addContainer = document.getElementById('additional-bots-container');
    const sidebarList = document.getElementById('bot-list');
    mainContainer.innerHTML = "";
    addContainer.innerHTML = "";
    sidebarList.innerHTML = "";

    // Always include User and System in CHAT sidebar
    ['User', 'System'].forEach(name => {
        const item = document.createElement('div');
        item.className = 'bot-item';
        item.dataset.name = name;
        item.innerHTML = `<div class="bot-status idle"></div><span>${name}</span>`;
        sidebarList.appendChild(item);
    });

    for (let index = 0; index < bots.length; index++) {
        const bot = bots[index];

        // Sidebar item (Initial state)
        const item = document.createElement('div');
        item.className = 'bot-item';
        item.dataset.name = bot.name;
        item.innerHTML = `<div class="bot-status offline"></div><span title="Connecting...">${bot.name}</span>`;
        sidebarList.appendChild(item);

        // Background fetch for model info
        (async (idx, name, el) => {
            try {
                const model = await GetBotModel(idx);
                el.querySelector('.bot-status').className = 'bot-status idle';
                el.querySelector('span').title = model;
            } catch (err) {
                el.querySelector('span').title = "Offline/Unknown";
            }
        })(index, bot.name, item);

        // Bot card in Bots tab
        const card = document.createElement('div');
        card.className = 'bot-card';
        card.innerHTML = `
            <div class="bot-card-header">
                <div style="display: flex; align-items: center; gap: 2px; flex: 1;">
                    <span style="font-weight: bold; color: #888;">@</span>
                    <input type="text" value="${bot.name}" placeholder="Bot Name" class="bot-name" style="font-weight: bold; flex: 1;">
                </div>
                ${index > 0 ? `
                    <div style="display: flex; align-items: center; gap: 10px;">
                        <span style="font-size: 0.8em; color: #888;">(toolbot)</span>
                        <button class="remove-bot-btn" style="background-color: #d32f2f; padding: 4px 8px;">Remove</button>
                    </div>
                ` : '<span>(Main AI/Coder)</span>'}
            </div>
            <div class="setting-group">
                <label>Server URL:</label>
                <div style="display: flex; gap: 10px;">
                    <input type="text" value="${bot.url}" placeholder="http://127.0.0.1:8080" class="bot-url" style="flex: 1;">
                    <button class="test-bot-btn">Test</button>
                </div>
            </div>
            <div class="setting-group">
                <label>Personality:</label>
                <textarea class="bot-personality" style="height: 60px;">${bot.personality}</textarea>
            </div>
            <div style="display: flex; gap: 20px;">
                <div class="setting-group" style="flex: 1;">
                    <label>Temperature: <span class="temp-val">${bot.temperature}</span></label>
                    <input type="range" min="0" max="2" step="0.1" value="${bot.temperature}" class="bot-temp">
                </div>
                <div class="setting-group" style="flex: 1;">
                    ${index > 0 ? `
                        <label>Triggers:</label>
                        <label style="font-size: 0.8em;"><input type="checkbox" ${bot.on_write ? 'checked' : ''} class="bot-on-write"> On File Write</label>
                        <input type="text" value="${bot.trigger_command || ''}" placeholder="Message to me: @botname [file]" class="bot-trigger-cmd" style="font-size: 0.8em; margin-top: 5px;">
                        <input type="text" value="${bot.save_output_command || ''}" placeholder="Command on reply: filewrite: append check.txt [reply]" class="bot-save-output" style="font-size: 0.8em; margin-top: 5px;">
                    ` : '<label>Triggers:</label><span style="font-size: 0.8em; color: #888;">(Primary bot triggers others)</span>'}
                </div>
            </div>
        `;

        card.querySelector('.bot-temp').oninput = (e) => {
            card.querySelector('.temp-val').textContent = e.target.value;
        };

        if (index > 0) {
            card.querySelector('.remove-bot-btn').onclick = () => {
                card.remove();
            };
        }

        card.querySelector('.test-bot-btn').onclick = async () => {
            try {
                const model = await GetBotModel(index);
                showNotification(`Success: ${model}`);
                // Update sidebar tooltip and status
                const sidebarItem = document.querySelector(`.bot-item[data-name="${bot.name}"]`);
                if (sidebarItem) {
                    sidebarItem.querySelector('.bot-status').className = 'bot-status idle';
                    sidebarItem.querySelector('span').title = model;
                }
            } catch (err) {
                showNotification("Failed to connect: " + err);
                const sidebarItem = document.querySelector(`.bot-item[data-name="${bot.name}"]`);
                if (sidebarItem) {
                    sidebarItem.querySelector('.bot-status').className = 'bot-status offline';
                    sidebarItem.querySelector('span').title = "Offline/Unknown";
                }
            }
        };

        if (index === 0) mainContainer.appendChild(card);
        else addContainer.appendChild(card);
    }
}

document.getElementById('add-bot-btn').onclick = () => {
    const bots = [];
    document.querySelectorAll('.bot-card').forEach(card => {
        const triggerCmd = card.querySelector('.bot-trigger-cmd');
        const saveOutput = card.querySelector('.bot-save-output');
        bots.push({
            name: card.querySelector('.bot-name').value,
            url: card.querySelector('.bot-url').value,
            personality: card.querySelector('.bot-personality').value,
            temperature: parseFloat(card.querySelector('.bot-temp').value),
            on_write: card.querySelector('.bot-on-write').checked,
            trigger_command: triggerCmd ? triggerCmd.value : "",
            save_output_command: saveOutput ? saveOutput.value : ""
        });
    });
    bots.push({
        name: "New Bot",
        url: "http://127.0.0.1:8080",
        personality: "You are a helpful assistant.",
        temperature: 0.7,
        on_write: false,
        save_output_command: ""
    });
    initBots(bots);
};

document.getElementById('save-bots-btn').onclick = async () => {
    const bots = [];
    document.querySelectorAll('.bot-card').forEach(card => {
        const triggerCmd = card.querySelector('.bot-trigger-cmd');
        const saveOutput = card.querySelector('.bot-save-output');
        bots.push({
            name: card.querySelector('.bot-name').value,
            url: card.querySelector('.bot-url').value,
            personality: card.querySelector('.bot-personality').value,
            temperature: parseFloat(card.querySelector('.bot-temp').value),
            on_write: card.querySelector('.bot-on-write').checked,
            trigger_command: triggerCmd ? triggerCmd.value : "",
            save_output_command: saveOutput ? saveOutput.value : ""
        });
    });

    const currentConfig = await GetConfig();
    const config = { ...currentConfig, bots: bots };
    const result = await SaveSettings(config);
    showNotification(result);
    // Refresh sidebar
    initBots(bots);
};

document.getElementById('save-prompt-btn').onclick = async () => {
    const currentConfig = await GetConfig();
    const config = {
        ...currentConfig,
        code_prompt: document.getElementById('code-prompt').value
    };
    const result = await SaveSettings(config);
    showNotification(result);
};

async function initCodeSetup() {
    const specs = await GetSpecs();
    if (specs.is_admin) {
        document.getElementById('account-warning').style.display = 'block';
    }

    const config = await GetConfig();
    document.getElementById('project-folder').value = config.project_folder || "";
    document.getElementById('app-name').value = config.app_name || "";
    document.getElementById('build-command').value = config.build_command || "build {app}";
    document.getElementById('run-command').value = config.run_command || "{app}";
    document.getElementById('kill-command').value = config.kill_command || "kill {app}";
    document.getElementById('code-prompt').value = config.code_prompt || "";
    document.getElementById('todo-list').value = config.todo_list || "";
    document.getElementById('ai-notes').value = config.ai_notes || "";

    const todoInput = document.getElementById('todo-list');
    todoInput.oninput = async () => {
        await UpdateTodoList(todoInput.value);
    };

    const notesInput = document.getElementById('ai-notes');
    notesInput.oninput = async () => {
        await UpdateAINotes(notesInput.value);
    };
}

async function refreshTools() {
    const config = await GetConfig();
    const tools = await ListAvailableTools();
    const toolsContainer = document.getElementById('tools-checkboxes');
    toolsContainer.innerHTML = "";
    tools.forEach(t => {
        const div = document.createElement('div');
        div.style.display = 'flex';
        div.style.alignItems = 'center';
        div.style.gap = '5px';
        div.className = 'tool-checkbox-item';

        const cb = document.createElement('input');
        cb.type = 'checkbox';
        cb.value = t.name;
        cb.id = `tool-${t.name}`;

        const roTools = ['help', 'todo', 'done', 'resume'];
        if (roTools.includes(t.name)) {
            cb.disabled = true;
            if (t.name === 'resume') {
                cb.checked = false;
            } else {
                cb.checked = true;
            }
        } else if (config.allowed_tools && config.allowed_tools.includes(t.name)) {
            cb.checked = true;
        }

        const lbl = document.createElement('label');
        lbl.htmlFor = `tool-${t.name}`;
        lbl.textContent = t.name;
        lbl.title = t.description; // Tooltip

        div.appendChild(cb);
        div.appendChild(lbl);
        toolsContainer.appendChild(div);
    });
}

document.getElementById('save-code-setup-btn').onclick = async () => {
    const currentConfig = await GetConfig();
    const config = {
        ...currentConfig,
        project_folder: document.getElementById('project-folder').value,
        app_name: document.getElementById('app-name').value,
        build_command: document.getElementById('build-command').value,
        run_command: document.getElementById('run-command').value,
        kill_command: document.getElementById('kill-command').value,
    };
    const result = await SaveSettings(config);
    showNotification(result);
};

document.getElementById('reset-prompt-btn').onclick = async () => {
    const defaultPrompt = await GetDefaultCodePrompt();
    document.getElementById('code-prompt').value = defaultPrompt;
};

let isCodeRunning = false;

document.getElementById('code-start-btn').onclick = async () => {
    const todo = document.getElementById('todo-list').value;
    if (!todo) {
        showNotification("Please provide a todo list first.");
        return;
    }

    currentTargetBot = "Main";
    updateBotStatus(currentTargetBot, 'thinking');

    await StartNewSession();
    await UpdateTodoList(todo);
    await ClearHistory();
    chatWindow.innerHTML = "";
    chatInput.disabled = true;
    sendBtn.disabled = true;
    document.getElementById('clear-btn').disabled = true;

    isCodeRunning = true;
    document.getElementById('code-status').textContent = "Mode: Started";
    await SetCodeActive(true);
    appendMessage('user', `Starting Code Mode...`);

    try {
        await SendCodeMessage("System: Code mode started. Please begin by exploring the project and checking the todo list.");
    } catch (err) {
        appendMessage('ai', `Error: ${err}`);
        await stopCodeMode();
    }
};

document.getElementById('code-stop-btn').onclick = async () => {
    await stopCodeMode();
    document.getElementById('code-status').textContent = "Mode: Stopped";
};

document.getElementById('code-resume-btn').onclick = async () => {
    const todo = document.getElementById('todo-list').value;
    currentTargetBot = "Main";
    updateBotStatus(currentTargetBot, 'thinking');
    await UpdateTodoList(todo);
    chatInput.disabled = true;
    sendBtn.disabled = true;
    document.getElementById('clear-btn').disabled = true;

    isCodeRunning = true;
    document.getElementById('code-status').textContent = "Mode: Resumed";
    await SetCodeActive(true);

    try {
        await SendCodeMessage("Resuming task: " + todo);
    } catch (err) {
        appendMessage('ai', `Error: ${err}`);
        await stopCodeMode();
    }
};

async function stopCodeMode() {
    isCodeRunning = false;
    await SetCodeActive(false);
    chatInput.disabled = false;
    sendBtn.disabled = false;
    document.getElementById('clear-btn').disabled = false;
    document.getElementById('code-status').textContent = "Mode: Stopped";
    showNotification("Code mode stopped/finished.");
}

document.getElementById('save-settings-btn').onclick = async () => {
    const allowed_tools = [];
    document.querySelectorAll('#tools-checkboxes input[type="checkbox"]').forEach(cb => {
        if (cb.checked) allowed_tools.push(cb.value);
    });

    const currentConfig = await GetConfig();
    const config = {
        ...currentConfig,
        memory_limit: parseInt(document.getElementById('memory-limit').value),
        remember_first: document.getElementById('remember-first').checked,
        debug_log: document.getElementById('debug-log').checked,
        allowed_tools: allowed_tools
    };
    const result = await SaveSettings(config);
    showNotification(result);
};

// Intercept link clicks to open in external browser
document.addEventListener('click', (e) => {
    const link = e.target.closest('a');
    if (link && link.href && (link.href.startsWith('http://') || link.href.startsWith('https://'))) {
        e.preventDefault();
        BrowserOpenURL(link.href);
    }
});

initSettings();
