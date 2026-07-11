const state = {
  items: [],
  editingId: null,
};

const els = {
  addForm: document.querySelector("#addForm"),
  nameInput: document.querySelector("#nameInput"),
  noteInput: document.querySelector("#noteInput"),
  itemsList: document.querySelector("#itemsList"),
  emptyState: document.querySelector("#emptyState"),
  template: document.querySelector("#itemTemplate"),
  totalCount: document.querySelector("#totalCount"),
  openCount: document.querySelector("#openCount"),
  doneCount: document.querySelector("#doneCount"),
  message: document.querySelector("#message"),
  clearCompleted: document.querySelector("#clearCompleted"),
};

async function api(path, options = {}) {
  const response = await fetch(path, {
    headers: {
      "Content-Type": "application/json",
      ...(options.headers || {}),
    },
    ...options,
  });

  if (!response.ok) {
    let message = "Nie udało się wykonać operacji.";
    try {
      const body = await response.json();
      message = body.error || message;
    } catch {
      // Keep the generic message when the response is not JSON.
    }
    throw new Error(message);
  }

  if (response.status === 204) {
    return null;
  }
  return response.json();
}

async function loadItems() {
  try {
    state.items = await api("/api/items");
    render();
  } catch (error) {
    showMessage(error.message, true);
  }
}

async function addItem(event) {
  event.preventDefault();
  const name = els.nameInput.value.trim();
  const note = els.noteInput.value.trim();
  if (!name) {
    els.nameInput.focus();
    return;
  }

  setBusy(els.addForm, true);
  try {
    const item = await api("/api/items", {
      method: "POST",
      body: JSON.stringify({ name, note }),
    });
    state.items.push(item);
    els.addForm.reset();
    els.nameInput.focus();
    showMessage("Dodano pozycję.");
    render();
  } catch (error) {
    showMessage(error.message, true);
  } finally {
    setBusy(els.addForm, false);
  }
}

async function updateItem(id, patch) {
  const index = state.items.findIndex((item) => item.id === id);
  if (index === -1) return;

  const previous = { ...state.items[index] };
  state.items[index] = { ...state.items[index], ...patch };
  render();

  try {
    const saved = await api(`/api/items/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
    state.items[index] = saved;
    render();
  } catch (error) {
    state.items[index] = previous;
    render();
    showMessage(error.message, true);
  }
}

async function deleteItem(id) {
  const item = state.items.find((entry) => entry.id === id);
  if (!item) return;
  if (!window.confirm(`Usunąć "${item.name}" z listy?`)) return;

  const previous = [...state.items];
  state.items = state.items.filter((entry) => entry.id !== id);
  render();

  try {
    await api(`/api/items/${encodeURIComponent(id)}`, { method: "DELETE" });
    showMessage("Usunięto pozycję.");
  } catch (error) {
    state.items = previous;
    render();
    showMessage(error.message, true);
  }
}

async function clearCompleted() {
  const completed = state.items.filter((item) => item.completed).length;
  if (completed === 0) return;
  if (!window.confirm(`Usunąć kupione pozycje (${completed})?`)) return;

  const previous = [...state.items];
  state.items = state.items.filter((item) => !item.completed);
  render();

  try {
    const result = await api("/api/items/clear-completed", { method: "POST" });
    showMessage(`Wyczyszczono kupione: ${result.removed}.`);
  } catch (error) {
    state.items = previous;
    render();
    showMessage(error.message, true);
  }
}

function render() {
  const total = state.items.length;
  const done = state.items.filter((item) => item.completed).length;
  const open = total - done;

  els.totalCount.textContent = total;
  els.openCount.textContent = open;
  els.doneCount.textContent = done;
  els.clearCompleted.disabled = done === 0;
  els.emptyState.hidden = total !== 0;
  els.itemsList.innerHTML = "";

  const sorted = [...state.items].sort((a, b) => {
    if (a.completed !== b.completed) return Number(a.completed) - Number(b.completed);
    return new Date(a.createdAt) - new Date(b.createdAt);
  });

  for (const item of sorted) {
    els.itemsList.appendChild(renderItem(item));
  }
}

function renderItem(item) {
  const node = els.template.content.firstElementChild.cloneNode(true);
  node.dataset.id = item.id;
  node.classList.toggle("is-completed", item.completed);
  node.classList.toggle("is-editing", state.editingId === item.id);

  const checkbox = node.querySelector(".item-check");
  const name = node.querySelector(".item-name");
  const note = node.querySelector(".item-note");
  const editForm = node.querySelector(".edit-form");
  const editName = node.querySelector(".edit-name");
  const editNote = node.querySelector(".edit-note");
  const editButton = node.querySelector(".edit-button");
  const saveButton = node.querySelector(".save-button");
  const cancelButton = node.querySelector(".cancel-button");
  const deleteButton = node.querySelector(".delete-button");

  checkbox.checked = item.completed;
  name.textContent = item.name;
  note.textContent = item.note || "Bez notatki";
  note.hidden = !item.note;
  editName.value = item.name;
  editNote.value = item.note || "";

  checkbox.addEventListener("change", () => updateItem(item.id, { completed: checkbox.checked }));
  editButton.addEventListener("click", () => {
    state.editingId = item.id;
    render();
    const current = els.itemsList.querySelector(`[data-id="${CSS.escape(item.id)}"] .edit-name`);
    current?.focus();
    current?.select();
  });
  cancelButton.addEventListener("click", () => {
    state.editingId = null;
    render();
  });
  saveButton.addEventListener("click", () => saveEdit(item.id, editName, editNote));
  deleteButton.addEventListener("click", () => deleteItem(item.id));
  editForm.addEventListener("submit", (event) => {
    event.preventDefault();
    saveEdit(item.id, editName, editNote);
  });

  const isEditing = state.editingId === item.id;
  editForm.hidden = !isEditing;
  node.querySelector(".read-view").hidden = isEditing;
  editButton.hidden = isEditing;
  saveButton.hidden = !isEditing;
  cancelButton.hidden = !isEditing;

  return node;
}

function saveEdit(id, nameInput, noteInput) {
  const name = nameInput.value.trim();
  const note = noteInput.value.trim();
  if (!name) {
    nameInput.focus();
    return;
  }

  state.editingId = null;
  updateItem(id, { name, note });
}

function setBusy(element, busy) {
  element.classList.toggle("is-busy", busy);
  for (const control of element.querySelectorAll("button, input")) {
    control.disabled = busy;
  }
}

let messageTimer;
function showMessage(text, isError = false) {
  window.clearTimeout(messageTimer);
  els.message.textContent = text;
  els.message.classList.toggle("is-error", isError);
  els.message.hidden = false;
  messageTimer = window.setTimeout(() => {
    els.message.textContent = "";
    els.message.hidden = true;
  }, 3200);
}

els.addForm.addEventListener("submit", addItem);
els.clearCompleted.addEventListener("click", clearCompleted);
loadItems();
