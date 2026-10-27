'use strict';

if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js');

const $ = (sel, root = document) => root.querySelector(sel);

// Close buttons and backdrop clicks close dialogs.
document.querySelectorAll('dialog').forEach(dlg => {
  dlg.addEventListener('click', e => {
    if (e.target === dlg || e.target.closest('.close')) dlg.close();
  });
});

// Keep the scroll position across the post/redirect round trip on the projection.
const ledger = $('#ledger');
if (ledger) {
  const y = sessionStorage.getItem('scrollY');
  if (y !== null) { window.scrollTo(0, +y); sessionStorage.removeItem('scrollY'); }
  document.querySelectorAll('dialog form').forEach(f =>
    f.addEventListener('submit', () => sessionStorage.setItem('scrollY', window.scrollY)));
}

// ------------------------------------------------------------ projection

const ed = $('#ed');
if (ed) {
  const f = $('form', ed);
  const open = (title, v) => {
    $('#ed-title').textContent = title;
    for (const k of ['kind', 'id', 'orig', 'date', 'desc', 'amount']) f.elements[k].value = v[k] || '';
    const shown = {
      rec: v.kind === 'rec' && v.skipped !== 'true',
      edited: v.kind === 'rec' && v.edited === 'true',
      'tx-existing': v.kind === 'tx' && v.id,
    };
    f.querySelectorAll('[data-for]').forEach(b => { b.hidden = !shown[b.dataset.for]; });
    $('#ed-hint').textContent = v.kind === 'rec'
      ? 'Changes apply to this occurrence only (scheduled ' + v.orig + ').'
        + (v.skipped === 'true' ? ' It is currently skipped; Save or Restore to bring it back.' : '')
      : '';
    ed.showModal();
  };

  ledger.addEventListener('click', e => {
    const tr = e.target.closest('tr[data-kind]');
    if (tr) open(tr.dataset.kind === 'rec' ? 'Edit occurrence' : 'Edit transaction', tr.dataset);
  });

  $('#add-tx').addEventListener('click', () => {
    const d = new Date();
    const today = new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 10);
    open('Add transaction', { kind: 'tx', date: today });
  });

  $('#rc-open').addEventListener('click', () => {
    const rc = $('#rc');
    rc.showModal();
    $('input[name=balance]', rc).focus();
  });
}

// ------------------------------------------------------------- recurring

const rows = $('#rows');
if (rows) {
  $('#addrow').addEventListener('click', () => {
    rows.appendChild($('#rowtpl').content.cloneNode(true));
    $('.rg:last-child input[name=desc]', rows).focus();
  });
  rows.addEventListener('click', e => {
    if (e.target.closest('.del')) e.target.closest('.rg').remove();
  });

  // Warn before leaving with unsaved row changes.
  let dirty = false;
  const form = $('#recf');
  form.addEventListener('input', () => { dirty = true; });
  rows.addEventListener('click', e => { if (e.target.closest('.del')) dirty = true; });
  $('#addrow').addEventListener('click', () => { dirty = true; });
  form.addEventListener('submit', () => { dirty = false; });
  window.addEventListener('beforeunload', e => { if (dirty) e.preventDefault(); });
}
