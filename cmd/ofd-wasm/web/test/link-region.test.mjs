import { sliceFunction, load, createSuite } from './harness.mjs';

const suite = createSuite('动作启动区域');
function element(tag) {
  return {
    tag, style: {}, attrs: {}, children: [], events: {},
    setAttribute(key, value) { this.attrs[key] = value; },
    append(child) { this.children.push(child); },
    replaceChildren() { this.children = []; },
    addEventListener(name, callback) { this.events[name] = callback; },
    getBoundingClientRect: () => ({ left: 10, top: 20, width: 80, height: 40 }),
  };
}
const layer = element('div');
const link = {boundary:{x:10,y:20,width:8,height:4},region:'M0 0L1 0L0 1Z',uri:'https://example.test/'};
const state = {
  pageCards:[{querySelector: () => layer}],
  pageInfos:[{width:40,height:40}],
  pageLinks:new Map([[0,[link]]]),
  pageRotation:0,
  document:{createElement:element,createElementNS:(_,tag)=>element(tag)},
  openPageLink:(value,point)=>{state.clicked={value,point};},
  showLinkPreview() {}, hideLinkPreview() {},
};
load(state,sliceFunction('applyPageLinks'),{expose:{apply:'applyPageLinks'}});
state.apply(0);
const svg=layer.children[0], button=layer.children[1];
suite.check(svg.tag==='svg' && svg.children[0].attrs.clipPathUnits==='objectBoundingBox', '使用归一化 SVG 裁剪，不把三角形扩大为矩形');
suite.check(svg.children[0].children[0].attrs.d===link.region && button.style.clipPath==='url(#ofd-link-region-0-0)', '热区绑定引擎返回的原始路径');
suite.check(button.style.left==='25%' && button.style.width==='20%', '按页面毫米尺寸定位区域');
button.events.click({detail:1,clientX:30,clientY:30});
suite.check(state.clicked.point.x===12 && state.clicked.point.y===21, '指针位置映回页面毫米坐标');
state.pageRotation=90;
button.events.click({detail:1,clientX:30,clientY:30});
suite.check(state.clicked.point.x===12 && state.clicked.point.y===23, '旋转页面先逆变换再判断各动作区域');
button.events.click({detail:0});
suite.check(state.clicked.point===null, '键盘激活不伪造鼠标坐标');
state.apply(0);
suite.check(layer.children.length===2, '重建热区时同步清理旧裁剪定义');

const calls=[];
const sequence={
  documentGeneration:1,hideLinkPreview(){},
  document:{createElement:()=>({getContext:()=>({isPointInPath:(_path,x,y)=>x+y<1})})},
  Path2D:class{constructor(path){this.path=path;}},
  window:{open:uri=>calls.push(uri)},
};
load(sequence,sliceFunction('openPageLink'),{expose:{open:'openPageLink'}});
await sequence.open({actions:[
  {uri:'inside',boundary:link.boundary,region:link.region},
  {uri:'outside',boundary:{x:30,y:30,width:4,height:4}},
]},{x:11,y:21});
suite.check(calls.join(',')==='inside', '同一序列仅执行指针实际命中的动作');
calls.length=0;
await sequence.open({actions:[{uri:'triangle-corner',boundary:link.boundary,region:link.region}]},{x:17,y:23});
suite.check(calls.length===0, '路径外、外接矩形内不触发动作');

process.exit(suite.report()?1:0);
