package main

// Definition of the listNode structure for a doubly linked list
type listNode struct {
	//Node pointing to the previous node of the current node.
	prev *listNode
	//Node pointing to the successor node of the current node.
	next *listNode
	//Record information about the value stored in the current node.
	value *interface{}
}

type list struct {
	//Points to the first node of the doubly linked list
	head *listNode
	//points to the last node of the linked list.
	tail *listNode
	//Record the current length of the doubly linked list
	len int64
}

func listCreate() *list {
	//Allocate memory space for the doubly linked list
	var l *list
	l = new(list)

	//Initialize the head and tail pointers.
	l.head = nil
	l.tail = nil

	//Initialize the length to 0, indicating that the current linked list has no nodes
	l.len = 0

	return l
}

func listAddNodeHead(l *list, value *interface{}) *list {
	//Allocate memory for a new node and set its value.
	var node *listNode
	node = new(listNode)
	node.value = value
	//If the length is 0, then both the head and tail pointers point to the new node.
	if l.len == 0 {
		l.head = node
		l.tail = node
	} else {
		//Make the original head node the successor node of the new node, node.
		node.prev = nil
		node.next = l.head
		l.head.prev = node
		l.head = node
	}
	//Maintain the information about the length of the linked list.
	l.len++
	return l
}

func listAddNodeTail(l *list, value *interface{}) *list {
	//Allocate memory for a new node and set its value.
	var node *listNode
	node = new(listNode)
	node.value = value
	//If the length is 0, then both the head and tail pointers point to the new node.
	if l.len == 0 {
		l.head = node
		l.tail = node
	} else {
		//Append the newly added node after the tail node to become the new tail node.
		node.prev = l.tail
		node.next = nil
		l.tail.next = node
		l.tail = node
	}
	//Maintain the information about the length of the linked list.
	l.len++
	return l

}

func listInsertNode(l *list, old_node *listNode, value *interface{}, after bool) *list {
	//Allocate memory for a new node and set its value.
	var node *listNode
	node = new(listNode)
	node.value = value
	//If after is true, insert the new node after the old node.
	if after {
		node.prev = old_node
		node.next = old_node.next
		//If the old node was originally the tail node, after the modification,
		//make the node the new tail node.
		if l.tail == old_node {
			l.tail = node
		}
	} else {
		//Add the new node before the old node.
		node.next = old_node
		node.prev = old_node.prev
		//If the original node is the head, then set the new node as the head
		if l.head == old_node {
			l.head = node

		}
	}
	//If the node's predecessor node is not empty, then point the predecessor to the node.
	if node.prev != nil {
		node.prev.next = node
	}
	//If the node's successor node is not empty, make this successor point to the node.
	if node.next != nil {
		node.next.prev = node
	}
	//Maintain the information about the length of the linked list.
	l.len++
	return l

}

func listDelNode(l *list, node *listNode) {
	//If the predecessor node is not empty,
	//then the predecessor node's next points to the successor node of the node being deleted
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		//If the deleted node is the head node, set the head to point to the next node.
		l.head = node.next
	}

	//If next is not empty, then let next point to the node before the deleted node
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		//If the deleted node is the tail node, make
		//the node before the deleted node the new tail node.
		l.tail = node.prev
	}
	//help gc
	node.prev = nil
	node.next = nil

	l.len--

}

func listIndex(l *list, index int64) *listNode {
	var n *listNode
	//"If less than 0, calculate the index value as a positive number n,
	//then continuously jump to the node pointed to by prev based on this positive number n.
	if index < 0 {
		index = (-index) - 1
		n = l.tail

		for index > 0 && n != nil {
			n = n.prev
			index--
		}
	} else {
		//Conversely, walk n steps from the front and reach the target node via next, then return.
		n = l.head
		for index > 0 && n != nil {
			n = n.next
			index--
		}
	}

	return n
}

func listLength(l *list) int64 {
	return l.len
}

func listFirst(l *list) *listNode {
	return l.head

}

func listNextNode(node *listNode) *listNode {
	return node.next
}

func listRelease(l **list) {
	*l = nil
}
