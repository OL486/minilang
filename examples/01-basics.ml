// 01-basics.ml —— 字面量、运算符、字符串
//
// 验收命令:
//   minilang run examples/01-basics.ml

// 优先级: 4/2 = 2, 2 % 3 = 2, 所以 1 + 6 - 2 = 5
let n = 1 + 2 * 3 - 4 / 2 % 3;
print n;

print (1 + 2) * 3;
print -5 + 1;
print !false;

print "hello" + ", " + "minilang";

// 只有 false 和 nil 是假；0 和空串都是真
if (0) { print "0 是真值"; }
if ("") { print "空串也是真值"; }

// 比较：不同类型之间 == 返回 false 而不报错，< 这类必须同类型
print 1 == 1.0;
print 1 == "1";
print "abc" < "abd";

// 短路求值：右边不会被求值，所以不会报「未定义变量」
print false && undefined_thing;
print true || undefined_thing;

print nil;
print 7 % 3;
print 7 / 2;
