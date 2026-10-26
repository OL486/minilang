// 05-gc-stress.ml —— 垃圾回收压力测试（S9 验收）
//
// 验收命令:
//   minilang run examples/05-gc-stress.ml --stats
//   minilang run examples/05-gc-stress.ml --stats --gc-off   // 对比：堆会一路涨上去
//
// 预期（本机参考值）：
//   --stats          存活对象 ~300，堆 <1 MiB，GC 次数 >10
//   --stats --gc-off 存活对象 = 累计创建数，堆 >20 MiB

// 1) 大量创建闭包：每次 make 都分配一个闭包 + 一个 upvalue
fn make(n) {
  fn get() { return n; }
  return get;
}

let i = 0;
let last = nil;
while (i < 200000) {
  last = make(i);
  i = i + 1;
}
print last();

// 2) 大量创建字符串：每次拼接都产生一个新的驻留字符串
let j = 0;
let s = "";
while (j < 3000) {
  s = s + "x";
  j = j + 1;
}
print s == s;

// 3) 短生命周期对象：块作用域退出后应当被回收
let k = 0;
while (k < 50000) {
  {
    let tmp = "临时对象" + "x";
    k = k + 1;
  }
}
print k;
