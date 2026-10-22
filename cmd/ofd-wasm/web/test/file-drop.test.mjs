import { sliceFunction, load, createSuite, viewerSource } from './harness.mjs';

const suite = createSuite('全界面文件拖放');
const opened = [];
const statuses = [];
const state = {
  openSelectedFile: file => opened.push(file),
  setStatus: message => statuses.push(message),
  fileDropOverlay: { hidden: true },
  empty: { classes: new Set(), classList: {
    add(name) { state.empty.classes.add(name); },
    remove(name) { state.empty.classes.delete(name); },
  } },
  dropHint: { style: { display: 'none' } },
};
load(state, ['isOFDFileDrag', 'handleOFDFileDrop', 'clearFileDropState'].map(sliceFunction).join('\n'), {
  expose: {
    isOFDFileDrag: 'isOFDFileDrag',
    handleOFDFileDrop: 'handleOFDFileDrop',
    clearFileDropState: 'clearFileDropState',
  },
});

suite.check(state.isOFDFileDrag({ dataTransfer: { types: ['Files'] } }), '识别操作系统文件拖放');
suite.check(!state.isOFDFileDrag({ dataTransfer: { types: ['text/plain'] } }), '忽略普通文本拖放');
state.handleOFDFileDrop({ dataTransfer: { files: [{ name: 'replacement.OFD' }, { name: 'other.txt' }] } });
suite.check(opened.length === 1 && opened[0].name === 'replacement.OFD', '已打开文档时也能通过 drop 替换打开 OFD');
state.handleOFDFileDrop({ dataTransfer: { files: [{ name: 'other.txt' }] } });
suite.check(statuses.at(-1) === '请拖入 OFD 文件。' && opened.length === 1, '拒绝非 OFD 文件并提示');
state.fileDropOverlay.hidden = false;
state.empty.classes.add('drag-over');
state.dropHint.style.display = 'block';
state.clearFileDropState();
suite.check(state.fileDropOverlay.hidden && !state.empty.classes.has('drag-over') && state.dropHint.style.display === 'none', 'drop 后清除拖入提示状态');
suite.check(viewerSource().includes("document.addEventListener('drop'") && viewerSource().includes("document.addEventListener('dragover'"), '拖放监听覆盖整个文档界面');

process.exit(suite.report() ? 1 : 0);
