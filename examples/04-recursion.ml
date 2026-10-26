// 04-recursion.ml —— 递归、局部函数递归、斐波那契
//
// 验收命令:
//   minilang run examples/04-recursion.ml
//   minilang run examples/04-recursion.ml --engine=tree   // 两个引擎输出必须一致

fn fib(n) {
  if (n < 2) { return n; }
  return fib(n - 1) + fib(n - 2);
}
print fib(10);
print fib(20);

// 局部函数递归：helper 是 fact 内部的局部变量，
// 编译器必须先声明槽位再编译函数体，否则 helper 解析不到自己
fn fact(n) {
  fn helper(n, acc) {
    if (n < 2) { return acc; }
    return helper(n - 1, acc * n);
  }
  return helper(n, 1);
}
print fact(10);
print fact(20);

// 尾递归形式（minilang 不做尾调用优化，这里只是写法演示）
fn sumTo(n) {
  fn loop(i, acc) {
    if (i > n) { return acc; }
    return loop(i + 1, acc + i);
  }
  return loop(1, 0);
}
print sumTo(100);

// 递归 + 闭包：每次递归都创建新闭包，旧闭包随作用域退出被 close
fn buildChain(n) {
  if (n == 0) {
    return nil;
  }
  let me = n;
  fn get() { return me; }
  print get();
  return buildChain(n - 1);
}
buildChain(3);
