// 06-clock-timing.ml —— 唯一的内置函数 clock：给一段代码计时
//
// 验收命令:
//   minilang run examples/06-clock-timing.ml
//   minilang run examples/06-clock-timing.ml --engine=tree   // 两个引擎输出必须一致
//
// clock() 返回当前 Unix 毫秒时间戳（number 类型），
// 是除 print 语句外语言里唯一的内置。
//
// 注意：这里只打印比较结果，不打印原始毫秒数 ——
// 两个引擎是先后两次运行，原始毫秒数必然不同，
// 只有比较结果才是确定性的，才能通过 `make examples` 的双引擎 diff。
// 想看真实耗时，用 `--stats` 跑 bench，或把 elapsed 打印出来自己看。

fn fib(n) {
  if (n < 2) { return n; }
  return fib(n - 1) + fib(n - 2);
}

let start = clock();
let result = fib(25);
let elapsed = clock() - start;

print result;        // 75025
print elapsed >= 0;  // true：后一次 clock() 不早于前一次

// 直接比较两次 clock()：时间只会往前走
let t1 = clock();
let t2 = clock();
print t2 >= t1;      // true

// clock 是普通全局变量，可以被覆盖（见 docs/02-pitfalls.md）：
// 下面这行只是演示语法，不真的覆盖它。
// let clock = 0;
