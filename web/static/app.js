const activeListStorageKey = "shopping-list.activeListId";

const state = {
  lists: [],
  items: [],
  activeListId: window.localStorage.getItem(activeListStorageKey) || null,
  editingId: null,
};

const els = {
  createListForm: document.querySelector("#createListForm"),
  newListName: document.querySelector("#newListName"),
  listsPanel: document.querySelector("#listsPanel"),
  renameListForm: document.querySelector("#renameListForm"),
  activeListName: document.querySelector("#activeListName"),
  deleteList: document.querySelector("#deleteList"),
  activeListTitle: document.querySelector("#activeListTitle"),
  composerListName: document.querySelector("#composerListName"),
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

function activeList() {
  return state.lists.find((list) => list.id === state.activeListId) || state.lists[0] || null;
}

function itemsPath() {
  const list = activeList();
  if (!list) throw new Error("Brak aktywnej listy.");
  return `/api/lists/${encodeURIComponent(list.id)}/items`;
}

async function loadLists(preferredListId = state.activeListId) {
  state.lists = await api("/api/lists");
  const preferred = state.lists.find((list) => list.id === preferredListId);
  state.activeListId = (preferred || state.lists[0] || {}).id || null;
  if (state.activeListId) {
    window.localStorage.setItem(activeListStorageKey, state.activeListId);
  } else {
    window.localStorage.removeItem(activeListStorageKey);
  }
}

async function loadItems() {
  const list = activeList();
  if (!list) {
    state.items = [];
    render();
    return;
  }
  state.items = await api(`/api/lists/${encodeURIComponent(list.id)}/items`);
  render();
}

async function refresh(preferredListId = state.activeListId) {
  try {
    await loadLists(preferredListId);
    await loadItems();
  } catch (error) {
    showMessage(error.message, true);
  }
}

async function selectList(id) {
  if (id === state.activeListId) return;
  state.activeListId = id;
  state.editingId = null;
  window.localStorage.setItem(activeListStorageKey, id);
  await loadItems();
}

async function createList(event) {
  event.preventDefault();
  const name = els.newListName.value.trim();
  if (!name) {
    els.newListName.focus();
    return;
  }

  setBusy(els.createListForm, true);
  try {
    const list = await api("/api/lists", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
    els.createListForm.reset();
    showMessage("Dodano listę.");
    await refresh(list.id);
  } catch (error) {
    showMessage(error.message, true);
  } finally {
    setBusy(els.createListForm, false);
  }
}

async function renameList(event) {
  event.preventDefault();
  const list = activeList();
  if (!list) return;

  const name = els.activeListName.value.trim();
  if (!name) {
    els.activeListName.focus();
    return;
  }

  setBusy(els.renameListForm, true);
  try {
    await api(`/api/lists/${encodeURIComponent(list.id)}`, {
      method: "PATCH",
      body: JSON.stringify({ name }),
    });
    showMessage("Zmieniono nazwę listy.");
    await refresh(list.id);
  } catch (error) {
    showMessage(error.message, true);
  } finally {
    setBusy(els.renameListForm, false);
  }
}

async function deleteActiveList() {
  const list = activeList();
  if (!list) return;
  if (!window.confirm(`Usunąć listę "${list.name}"?`)) return;

  try {
    await api(`/api/lists/${encodeURIComponent(list.id)}`, { method: "DELETE" });
    showMessage("Usunięto listę.");
    await refresh();
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
    const item = await api(itemsPath(), {
      method: "POST",
      body: JSON.stringify({ name, note }),
    });
    state.items.push(item);
    els.addForm.reset();
    els.nameInput.focus();
    showMessage("Dodano pozycję.");
    await loadLists(state.activeListId);
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
    const saved = await api(`${itemsPath()}/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
    state.items[index] = saved;
    await loadLists(state.activeListId);
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
    await api(`${itemsPath()}/${encodeURIComponent(id)}`, { method: "DELETE" });
    await loadLists(state.activeListId);
    showMessage("Usunięto pozycję.");
    render();
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
    const result = await api(`${itemsPath()}/clear-completed`, { method: "POST" });
    await loadLists(state.activeListId);
    showMessage(`Wyczyszczono kupione: ${result.removed}.`);
    render();
  } catch (error) {
    state.items = previous;
    render();
    showMessage(error.message, true);
  }
}

function render() {
  renderLists();

  const list = activeList();
  const total = state.items.length;
  const done = state.items.filter((item) => item.completed).length;
  const open = total - done;

  els.activeListTitle.textContent = list?.name || "Lista zakupów";
  els.composerListName.textContent = list?.name || "Lista zakupów";
  els.activeListName.value = list?.name || "";
  els.totalCount.textContent = total;
  els.openCount.textContent = open;
  els.doneCount.textContent = done;
  els.clearCompleted.disabled = done === 0 || !list;
  els.addForm.querySelector("button[type='submit']").disabled = !list;
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

function renderLists() {
  els.listsPanel.innerHTML = "";
  for (const list of state.lists) {
    const button = document.createElement("button");
    button.className = "list-tab";
    button.type = "button";
    button.setAttribute("role", "option");
    button.setAttribute("aria-selected", String(list.id === state.activeListId));
    button.classList.toggle("is-active", list.id === state.activeListId);
    button.innerHTML = `<strong></strong><span></span>`;
    button.querySelector("strong").textContent = list.name;
    button.querySelector("span").textContent = `${list.openCount} do kupienia, ${list.doneCount} kupione`;
    button.addEventListener("click", () => selectList(list.id));
    els.listsPanel.appendChild(button);
  }
}

function renderItem(item) {
  const node = els.template.content.firstElementChild.cloneNode(true);
  node.dataset.id = item.id;
  node.classList.toggle("is-completed", item.completed);
  node.classList.toggle("is-editing", state.editingId === item.id);

  const checkbox = node.querySelector(".item-check");
  const visual = document.createElement("div");
  visual.className = "item-visual";
  visual.setAttribute("aria-hidden", "true");
  visual.textContent = visualForItem(item.name);
  checkbox.closest(".check-wrap").after(visual);
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

function visualForItem(name) {
  const text = name.toLocaleLowerCase("pl-PL");
  const dictionary = [
    [/kaw|coffee/, "C"],
    [/chleb|bread|buł|bul/, "B"],
    [/mlek|milk/, "M"],
    [/wod|water/, "W"],
    [/pomid|tomat/, "T"],
    [/banan/, "B"],
    [/jaj|egg/, "O"],
    [/ser|cheese/, "S"],
    [/szyn|ham/, "H"],
    [/makaron|pasta/, "P"],
    [/papier|toalet/, "O"],
    [/żel|zel|myd|soap|szampon/, "S"],
  ];
  const match = dictionary.find(([pattern]) => pattern.test(text));
  if (match) return match[1];
  const letter = [...name.trim()][0] || "•";
  return letter.toLocaleUpperCase("pl-PL");
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

els.createListForm.addEventListener("submit", createList);
els.renameListForm.addEventListener("submit", renameList);
els.deleteList.addEventListener("click", deleteActiveList);
els.addForm.addEventListener("submit", addItem);
els.clearCompleted.addEventListener("click", clearCompleted);
refresh();
