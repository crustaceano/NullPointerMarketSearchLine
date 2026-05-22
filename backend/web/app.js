const form = document.getElementById("search-form");
const submitBtn = document.getElementById("submit-btn");
const statusLine = document.getElementById("status-line");
const resultsEl = document.getElementById("results");
const normSection = document.getElementById("normalization");

form.addEventListener("submit", async (e) => {
  e.preventDefault();
  const query = document.getElementById("query").value.trim();
  const region = document.getElementById("region").value.trim() || "Москва";
  if (!query) return;

  setBusy(true);
  resultsEl.innerHTML = "";
  normSection.classList.add("hidden");
  statusLine.textContent = "Ищем предложения…";

  try {
    const url = `/search?query=${encodeURIComponent(query)}&region=${encodeURIComponent(region)}`;
    const resp = await fetch(url);
    if (!resp.ok) {
      const err = await resp.json().catch(() => ({}));
      throw new Error(err.error || `HTTP ${resp.status}`);
    }
    const data = await resp.json();
    renderNormalization(data.normalization);
    renderSources(data.sources);
    statusLine.textContent = summarize(data.sources);
  } catch (err) {
    statusLine.textContent = "Ошибка: " + err.message;
  } finally {
    setBusy(false);
  }
});

function setBusy(busy) {
  submitBtn.disabled = busy;
  submitBtn.textContent = busy ? "Ищем…" : "Искать";
}

function renderNormalization(n) {
  if (!n) return;
  document.getElementById("n-raw").textContent = n.raw || "";
  document.getElementById("n-corrected").textContent = n.corrected || "";
  document.getElementById("n-synonyms").innerHTML = renderChips(n.synonyms);
  document.getElementById("n-expanded").innerHTML = renderChips(n.expanded_queries);
  normSection.classList.remove("hidden");
}

function renderChips(items) {
  if (!items || items.length === 0) return '<span class="empty-msg">—</span>';
  return items.map((s) => `<span class="chip">${escapeHTML(s)}</span>`).join(" ");
}

function renderSources(sources) {
  resultsEl.innerHTML = "";
  if (!sources || sources.length === 0) {
    resultsEl.innerHTML = '<p class="empty-msg">Источники не вернули результатов.</p>';
    return;
  }
  for (const src of sources) {
    resultsEl.appendChild(renderSourceBlock(src));
  }
}

function renderSourceBlock(src) {
  const block = document.createElement("section");
  block.className = "source-block";

  const header = document.createElement("div");
  header.className = "source-header";
  header.innerHTML = `
    <h3>${escapeHTML(src.source)}</h3>
    <span class="status-badge status-${src.status}">${statusLabel(src.status)}</span>
  `;
  block.appendChild(header);

  if (src.status === "error") {
    const p = document.createElement("p");
    p.className = "error-msg";
    p.textContent = src.error || "Источник недоступен.";
    block.appendChild(p);
    return block;
  }
  if (src.status === "empty" || !src.offers || src.offers.length === 0) {
    const p = document.createElement("p");
    p.className = "empty-msg";
    p.textContent = "Нет предложений по запросу.";
    block.appendChild(p);
    return block;
  }

  const cards = document.createElement("div");
  cards.className = "cards";
  for (const offer of src.offers) {
    cards.appendChild(renderOfferCard(offer));
  }
  block.appendChild(cards);
  return block;
}

function renderOfferCard(offer) {
  const card = document.createElement("article");
  card.className = "card";

  const chars = Object.entries(offer.characteristics || {})
    .map(([k, v]) => `<div><strong>${escapeHTML(k)}:</strong> ${escapeHTML(String(v))}</div>`)
    .join("");

  card.innerHTML = `
    <img src="${escapeAttr(offer.image)}" alt="${escapeAttr(offer.title)}" loading="lazy" />
    <div class="title">${escapeHTML(offer.title)}</div>
    <div class="price">${formatPrice(offer.price, offer.currency)}</div>
    <div class="chars">${chars}</div>
    <a href="${escapeAttr(offer.url)}" target="_blank" rel="noopener noreferrer">Открыть на источнике →</a>
  `;
  return card;
}

function statusLabel(status) {
  if (status === "success") return "success";
  if (status === "empty") return "empty";
  if (status === "error") return "error";
  return status;
}

function summarize(sources) {
  const total = sources.reduce((acc, s) => acc + (s.offers ? s.offers.length : 0), 0);
  const ok = sources.filter((s) => s.status === "success").length;
  const fail = sources.filter((s) => s.status === "error").length;
  return `Источников: ${sources.length} · успешно: ${ok} · ошибок: ${fail} · всего предложений: ${total}`;
}

function formatPrice(price, currency) {
  if (price == null) return "—";
  try {
    return new Intl.NumberFormat("ru-RU", {
      style: "currency",
      currency: currency || "RUB",
      maximumFractionDigits: 0,
    }).format(price);
  } catch {
    return `${Math.round(price)} ${currency || ""}`.trim();
  }
}

function escapeHTML(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;",
  })[c]);
}
function escapeAttr(s) { return escapeHTML(s); }
