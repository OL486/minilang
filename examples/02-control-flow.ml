// 02-control-flow.ml —— if / else if / while / 作用域
//
// 验收命令:
//   minilang run examples/02-control-flow.ml

// S3 的验收用例
let i = 0;
while (i < 5) {
  print i;
  i = i + 1;
}

fn grade(score) {
  if (score >= 90) {
    return "A";
  } else if (score >= 60) {
    return "B";
  } else {
    return "C";
  }
}
print grade(95);
print grade(72);
print grade(30);

// 块引入新作用域，内层同名变量遮蔽外层，退出块后外层不受影响
let x = "outer";
{
  let x = "middle";
  print x;
  {
    let x = "inner";
    print x;
  }
  print x;
}
print x;

// while + 累加
let sum = 0;
let k = 1;
while (k <= 100) {
  sum = sum + k;
  k = k + 1;
}
print sum;

// 循环里 return
fn firstSquareOver(limit) {
  let n = 0;
  while (true) {
    if (n * n > limit) {
      return n;
    }
    n = n + 1;
  }
}
print firstSquareOver(50);
