// 03-closures.ml —— 函数是一等公民、闭包、upvalue
//
// 验收命令:
//   minilang run examples/03-closures.ml
//   minilang run examples/03-closures.ml --dump-bytecode   // 看 OP_CLOSURE 的 upvalue 描述

// S4 的验收用例：两个计数器必须互不干扰
fn makeCounter() {
  let count = 0;
  fn inc() {
    count = count + 1;
    return count;
  }
  return inc;
}

let c1 = makeCounter();
let c2 = makeCounter();
print c1(); // 1
print c1(); // 2
print c2(); // 1  <- 如果这里输出 3，说明两个闭包共享了同一个 upvalue
print c1(); // 3

// 函数可以存进变量、当参数传、当返回值
fn add(a, b) { return a + b; }
let f = add;
print f(2, 3);

fn apply(g, x) { return g(x); }
fn twice(n) { return n * 2; }
print apply(twice, 21);

// 三级嵌套：最内层同时捕获三层外的变量
fn outer(a) {
  fn middle(b) {
    fn inner(c) {
      return a + b + c;
    }
    return inner;
  }
  return middle;
}
print outer(1)(2)(3);

// 闭包捕获的是「变量」而不是「值」：
// 两个函数共享同一个 upvalue，所以 addOne 能看到 double 的修改
fn makePair() {
  let v = 0;
  fn bump() { v = v + 10; return v; }
  fn read() { return v; }
  return bump;
}
let bump = makePair();
print bump();
print bump();

// 返回的函数打印出来是 <fn 名字>
print makeCounter();
