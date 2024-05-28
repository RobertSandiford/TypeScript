//import { SyntaxKind } from "./types.js"


export function decToBin(dec: number): string {
    return dec.toString(2);
}

export function bitFlags(flags: number, enu: Record<string, string>): string {
    const flagsBinary = decToBin(flags)
    //console.log(flagsBinary)
    let result = ""
    //console.log(flagsBinary.length)
    const start = flagsBinary.length - 1
    for (let pos = 0; pos < flagsBinary.length; pos++) {
        //console.log(pos, flagsBinary[pos])
        if ((flagsBinary[pos]) === "1") {
            const shift = start - pos
            //console.log('bit at ', shift)
            if (result.length > 0) result += ', '
            result += enu[1 << shift]
        }
    }
    return result
}
export function showStack(message: string = "") {
    try {
        throw new Error(message)
    } catch (e) {
        if (e instanceof Error) {
            const stack = e.stack
            if (stack !== undefined) {
                const lines = stack.split('\n')
                //console.log(
                console.log([(message + ' stack').trim(), ...lines.slice(2)].join('\n'))
            }
        }
    }
}
// export const specialWords = ['closed', 'open', 'readonly', 'XX__Readonly']
// export const specialSyntaxKinds = [SyntaxKind.ClosedKeyword, SyntaxKind.OpenKeyword, SyntaxKind.ReadonlyKeyword]