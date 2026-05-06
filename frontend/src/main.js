import {GetSpecs, GetConfig, SaveSettings, ListModels, ListClips, GetBalancedLayers, SendMessage, ClearHistory, CheckServerExecutable, StartServer, StopServer, IsServerRunning, GetImageBase64, GetFileContent, ListAvailableTools, SendCodeMessage, UpdateTodoList, SetCodeActive, GetAINotes, UpdateAINotes, GetDefaultCodePrompt, TestTool} from '../wailsjs/go/main/App';
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
const sidebar = document.getElementById('preview-sidebar');
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

function appendMessage(role, content) {
    const div = document.createElement('div');
    div.className = `message ${role}`;
    div.id = role === 'ai' ? 'latest-ai-msg' : '';
    div.innerHTML = marked.parse(content);
    chatWindow.appendChild(div);
    chatWindow.scrollTop = chatWindow.scrollHeight;
    return div;
}

let currentAiMsgDiv = null;
let currentAiContent = "";

EventsOn('token', (token) => {
    if (!currentAiMsgDiv) {
        currentAiMsgDiv = appendMessage('ai', '');
    }
    currentAiContent += token;
    currentAiMsgDiv.innerHTML = marked.parse(currentAiContent);
    chatWindow.scrollTop = chatWindow.scrollHeight;
});

EventsOn('done', () => {
    currentAiMsgDiv = null;
    currentAiContent = "";
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

EventsOn('server-log', (log) => {
    const logArea = document.getElementById('server-log');
    logArea.value += log + "\n";
    logArea.scrollTop = logArea.scrollHeight;
});

EventsOn('server-status', (status) => {
    updateServerStatus(status);
});

document.getElementById('clear-log-btn').onclick = () => {
    document.getElementById('server-log').value = "";
};

sendBtn.onclick = async () => {
    const text = chatInput.value.trim();
    if (!text && !currentImagePath) return;

    const hasServer = await CheckServerExecutable();
    if (!hasServer) {
        appendMessage('ai', '### ⚠️ Missing Server or Backends\n\nPlease copy **ALL files** from the [llama.cpp zip](https://github.com/ggerganov/llama.cpp/releases) into the `llama/` folder.\n\nRequired:\n- `llama-server.exe`\n- `llama.dll`\n- `ggml-cpu.dll` (and other `ggml-*.dll` files)');
        return;
    }

    appendMessage('user', text);
    chatInput.value = "";

    try {
        await SendMessage(text, currentImagePath);
    } catch (err) {
        appendMessage('ai', `Error: ${err}`);
    }
};

document.getElementById('clear-btn').onclick = async () => {
    await ClearHistory();
    chatWindow.innerHTML = "";
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
    document.getElementById('personality-input').value = config.personality;
    document.getElementById('memory-limit').value = config.memory_limit;
    document.getElementById('remember-first').checked = config.remember_first;
    document.getElementById('debug-log').checked = config.debug_log;
    document.getElementById('gpu-layers').value = config.gpu_layers;
    document.getElementById('server-url').value = config.server_url;
    document.getElementById('server-mode').value = config.server_mode;

    const updateModeUI = () => {
        const mode = document.getElementById('server-mode').value;
        const isLocal = mode === 'local';

        document.querySelectorAll('.local-only').forEach(el => {
            el.style.display = isLocal ? 'flex' : 'none';
        });
        document.querySelectorAll('.remote-only').forEach(el => {
            el.style.display = isLocal ? 'none' : 'flex';
        });
    };
    document.getElementById('server-mode').onchange = updateModeUI;
    updateModeUI();

    const models = await ListModels();
    const modelSelect = document.getElementById('model-select');
    models.forEach(m => {
        const opt = document.createElement('option');
        opt.value = m;
        opt.textContent = m;
        if (m === config.model_path) opt.selected = true;
        modelSelect.appendChild(opt);
    });

    const clips = await ListClips();
    const clipSelect = document.getElementById('clip-select');
    clips.forEach(c => {
        const opt = document.createElement('option');
        opt.value = c;
        opt.textContent = c;
        if (c === config.clip_path) opt.selected = true;
        clipSelect.appendChild(opt);
    });

    modelSelect.onchange = async () => {
        const balanced = await GetBalancedLayers(modelSelect.value);
        document.getElementById('balanced-recommend').textContent = balanced;
    };
    modelSelect.onchange();

    await updateServerStatus();
    await initCodeSetup();
}

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

    await refreshTools();

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
        if (config.allowed_tools && config.allowed_tools.includes(t.name)) {
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

document.getElementById('save-tools-btn').onclick = async () => {
    const allowed_tools = [];
    document.querySelectorAll('#tools-checkboxes input[type="checkbox"]').forEach(cb => {
        if (cb.checked) allowed_tools.push(cb.value);
    });

    const currentConfig = await GetConfig();
    const config = {
        ...currentConfig,
        code_prompt: document.getElementById('code-prompt').value,
        allowed_tools: allowed_tools
    };
    const result = await SaveSettings(config);
    showNotification(result);
};

document.getElementById('reset-prompt-btn').onclick = async () => {
    const defaultPrompt = await GetDefaultCodePrompt();
    document.getElementById('code-prompt').value = defaultPrompt;
};

document.getElementById('code-test-btn').onclick = async () => {
    isTestMode = !isTestMode;
    if (isTestMode) {
        isCodeRunning = false;
        await SetCodeActive(false);
        chatInput.disabled = false;
        sendBtn.disabled = false;
        document.getElementById('clear-btn').disabled = false;
        document.getElementById('code-status').textContent = "Mode: Testing";

        const mainTabBtn = document.querySelector('button[onclick*="showTab(this, \'main\')"]');
        if (mainTabBtn) window.showTab(mainTabBtn, 'main');

        showNotification("Test Mode Enabled: Type commands directly in chat.");
    } else {
        document.getElementById('code-status').textContent = "Mode: Stopped";
        showNotification("Test Mode Disabled.");
    }
};

let isCodeRunning = false;
let isTestMode = false;

document.getElementById('code-start-btn').onclick = async () => {
    const todo = document.getElementById('todo-list').value;
    if (!todo) {
        showNotification("Please provide a todo list first.");
        return;
    }

    await StartNewSession();
    await UpdateTodoList(todo);
    await ClearHistory();
    chatWindow.innerHTML = "";
    chatInput.disabled = true;
    sendBtn.disabled = true;
    document.getElementById('clear-btn').disabled = true;

    // Switch to Main tab
    const mainTabBtn = document.querySelector('button[onclick*="showTab(this, \'main\')"]');
    if (mainTabBtn) window.showTab(mainTabBtn, 'main');

    isCodeRunning = true;
    isTestMode = false;
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
    await UpdateTodoList(todo);
    chatInput.disabled = true;
    sendBtn.disabled = true;
    document.getElementById('clear-btn').disabled = true;

    // Switch to Main tab
    const mainTabBtn = document.querySelector('button[onclick*="showTab(this, \'main\')"]');
    if (mainTabBtn) window.showTab(mainTabBtn, 'main');

    isCodeRunning = true;
    isTestMode = false;
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
    isTestMode = false;
    await SetCodeActive(false);
    chatInput.disabled = false;
    sendBtn.disabled = false;
    document.getElementById('clear-btn').disabled = false;
    showNotification("Code mode stopped/finished.");
}

async function updateServerStatus(statusText) {
    const running = await IsServerRunning();
    const text = statusText || (running ? 'Running' : 'Stopped');

    const isBooting = text === 'Booting...';

    // Disable/Enable buttons
    document.getElementById('start-server-btn').disabled = running || isBooting;
    document.getElementById('stop-server-btn').disabled = !running && !isBooting;

    const statusEl = document.getElementById('server-status-tab');
    if (statusEl) {
        statusEl.textContent = text;
        if (text === 'Running') statusEl.style.color = '#44ff44';
        else if (isBooting) statusEl.style.color = '#ffcc00';
        else statusEl.style.color = '#ff4444';
    }
}

document.getElementById('start-server-btn').onclick = async () => {
    const hasServer = await CheckServerExecutable();
    if (!hasServer) {
        showNotification('llama-server.exe or ggml-*.dll missing in llama/ folder. Copy all files from the llama.cpp zip.');
        return;
    }
    try {
        await StartServer();
        await updateServerStatus();
    } catch (err) {
        showNotification(`Error: ${err}`);
    }
};

document.getElementById('stop-server-btn').onclick = async () => {
    await StopServer();
    await updateServerStatus();
};

document.getElementById('test-connection-btn').onclick = async () => {
    const url = document.getElementById('server-url').value;
    const status = document.getElementById('connection-status');
    status.textContent = "Testing...";
    status.style.color = "white";

    try {
        const resp = await fetch(url + "/health");
        if (resp.ok) {
            status.textContent = "✅ Connected Successfully";
            status.style.color = "#44ff44";
            showNotification("Connected Successfully");
        } else {
            status.textContent = `❌ Server returned error: ${resp.status}`;
            status.style.color = "#ff4444";
            showNotification("Server returned error");
        }
    } catch (err) {
        status.textContent = `❌ Failed to connect: ${err}`;
        status.style.color = "#ff4444";
        showNotification("Failed to connect");
    }
};

document.getElementById('save-settings-btn').onclick = async () => {
    const config = {
        model_path: document.getElementById('model-select').value,
        clip_path: document.getElementById('clip-select').value,
        personality: document.getElementById('personality-input').value,
        memory_limit: parseInt(document.getElementById('memory-limit').value),
        remember_first: document.getElementById('remember-first').checked,
        debug_log: document.getElementById('debug-log').checked,
        gpu_layers: parseInt(document.getElementById('gpu-layers').value),
        server_url: document.getElementById('server-url').value,
        server_mode: document.getElementById('server-mode').value,
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
